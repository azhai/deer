package main

import (
	"fmt"
	"log/slog"
	"os"

	"tinygo.org/x/go-llvm"
)

var (
	ctx             = llvm.NewContext()
	builder         = ctx.NewBuilder()
	rootModule      = ctx.NewModule("root")
	options         = llvm.NewMCJITCompilerOptions()
	namedVals       = map[string]llvm.Value{}
	namedTypes      = map[string]llvm.Type{} // element type of each named alloca (needed for opaque pointers)
	execEngine      llvm.ExecutionEngine
	machine         llvm.TargetMachine
	llvmStructTypes = map[string]llvm.Type{} // struct name → LLVM type
)

func initExecutionEngine() {
	var err error
	var target llvm.Target

	llvm.LinkInMCJIT()

	err = llvm.InitializeNativeTarget()
	if err != nil {
		slog.Error("native target initialization failed", "error", err)
		os.Exit(1)
	}

	err = llvm.InitializeNativeAsmPrinter()
	if err != nil {
		slog.Error("ASM printer initialization failed", "error", err)
		os.Exit(1)
	}

	target, err = llvm.GetTargetFromTriple(llvm.DefaultTargetTriple())
	if err != nil {
		slog.Error("cannot get target", "error", err)
		os.Exit(1)
	}

	slog.Debug("initialized",
		"targetTriple", llvm.DefaultTargetTriple(),
		"target", target.Description(),
	)

	machine = target.CreateTargetMachine(llvm.DefaultTargetTriple(),
		"", "",
		llvm.CodeGenLevelNone,
		llvm.RelocDefault,
		llvm.CodeModelSmall)

	targetData := machine.CreateTargetData()
	slog.Debug("target machine created", "targetData", targetData.String())

	options.SetMCJITOptimizationLevel(2)
	options.SetMCJITEnableFastISel(true)
	options.SetMCJITNoFramePointerElim(true)
	options.SetMCJITCodeModel(llvm.CodeModelDefault)
	execEngine, err = llvm.NewMCJITCompiler(rootModule, options)
	if err != nil {
		slog.Error("JIT compiler initialization failed", "error", err)
		os.Exit(1)
	}

	slog.Debug("execution engine ready", "targetData", execEngine.TargetData().String())
	registerHostSymbols()
}

// registerHostSymbols maps the host C functions into the JIT so that
// JIT-compiled code can call them by name.
func registerHostSymbols() {
	for name, addr := range hostSymbolAddrs() {
		registerLLVMSymbol(name, addr)
	}
}

func Optimize() {
	opts := llvm.NewPassBuilderOptions()
	defer opts.Dispose()
	err := rootModule.RunPasses("mem2reg,instcombine,reassociate,gvn,simplifycfg", machine, opts)
	if err != nil {
		slog.Error("optimization passes failed", "error", err)
		os.Exit(1)
	}
}

// llvmTypeFor maps a Deer TypeKind to an LLVM type.
func llvmTypeFor(k TypeKind) llvm.Type {
	switch k {
	case TypeVoid:
		return ctx.VoidType()
	case TypeBool:
		return ctx.Int1Type()
	case TypeByte:
		return ctx.Int8Type()
	case TypeShort:
		return ctx.Int16Type()
	case TypeRune:
		return ctx.Int32Type()
	case TypeInt:
		return ctx.Int64Type()
	case TypeFloat:
		return ctx.DoubleType()
	default:
		return ctx.Int64Type()
	}
}

func createEntryBlockAlloca(f llvm.Value, name string, typ llvm.Type) llvm.Value {
	tmpB := ctx.NewBuilder()
	tmpB.SetInsertPoint(f.EntryBasicBlock(), f.EntryBasicBlock().FirstInstruction())
	return tmpB.CreateAlloca(typ, name)
}

func (n *fnPrototypeNode) createArgAlloca(f llvm.Value) {
	args := f.Params()
	for i := range args {
		argType := args[i].Type()
		alloca := createEntryBlockAlloca(f, n.args[i], argType)
		builder.CreateStore(args[i], alloca)
		namedVals[n.args[i]] = alloca
		namedTypes[n.args[i]] = argType
	}
}

func (n *numberNode) codegen() llvm.Value {
	if n.isInt {
		return llvm.ConstInt(ctx.Int64Type(), uint64(int64(n.val)), true)
	}
	return llvm.ConstFloat(ctx.DoubleType(), n.val)
}

func (n *variableNode) codegen() llvm.Value {
	v := namedVals[n.name]
	if v.IsNil() {
		return ErrorV(fmt.Sprintf("unknown variable name: %s at %s", n.name, n.SrcPos))
	}
	elemType := namedTypes[n.name]
	if elemType.IsNil() {
		return ErrorV(fmt.Sprintf("unknown type for variable: %s", n.name))
	}
	return builder.CreateLoad(elemType, v, n.name)
}

func (n *ifNode) codegen() llvm.Value {
	ifv := n.ifN.codegen()
	if ifv.IsNil() {
		return ErrorV("code generation failed for if condition")
	}
	// Convert condition to bool (i1): compare != 0.
	if ifv.Type() == ctx.Int64Type() {
		ifv = builder.CreateICmp(llvm.IntNE, ifv, llvm.ConstInt(ctx.Int64Type(), 0, false), "ifcond")
	} else if ifv.Type() == ctx.Int1Type() {
		// already bool
	} else {
		ifv = builder.CreateFCmp(llvm.FloatONE, ifv, llvm.ConstFloat(ctx.DoubleType(), 0), "ifcond")
	}

	parentFunc := builder.GetInsertBlock().Parent()
	thenBlk := llvm.AddBasicBlock(parentFunc, "then")
	elseBlk := llvm.AddBasicBlock(parentFunc, "else")
	mergeBlk := llvm.AddBasicBlock(parentFunc, "merge")
	builder.CreateCondBr(ifv, thenBlk, elseBlk)

	builder.SetInsertPointAtEnd(thenBlk)
	thenv := n.thenN.codegen()
	if thenv.IsNil() {
		return ErrorV("code generation failed for then expression")
	}
	builder.CreateBr(mergeBlk)
	thenBlk = builder.GetInsertBlock()

	builder.SetInsertPointAtEnd(elseBlk)
	elsev := n.elseN.codegen()
	if elsev.IsNil() {
		return ErrorV("code generation failed for else expression")
	}
	builder.CreateBr(mergeBlk)
	elseBlk = builder.GetInsertBlock()

	builder.SetInsertPointAtEnd(mergeBlk)
	// PHI type matches the branch result type.
	resultType := thenv.Type()
	phiNode := builder.CreatePHI(resultType, "iftmp")
	phiNode.AddIncoming([]llvm.Value{thenv}, []llvm.BasicBlock{thenBlk})
	phiNode.AddIncoming([]llvm.Value{elsev}, []llvm.BasicBlock{elseBlk})
	return phiNode
}

func (n *forNode) codegen() llvm.Value {
	startVal := n.start.codegen()
	if startVal.IsNil() {
		return ErrorV("code generation failed for start expression")
	}

	parentFunc := builder.GetInsertBlock().Parent()
	varType := startVal.Type()
	alloca := createEntryBlockAlloca(parentFunc, n.counter, varType)
	builder.CreateStore(startVal, alloca)
	loopBlk := llvm.AddBasicBlock(parentFunc, "loop")

	builder.CreateBr(loopBlk)
	builder.SetInsertPointAtEnd(loopBlk)

	oldVal := namedVals[n.counter]
	oldType := namedTypes[n.counter]
	namedVals[n.counter] = alloca
	namedTypes[n.counter] = varType

	if n.body.codegen().IsNil() {
		return ErrorV("code generation failed for body expression")
	}

	var stepVal llvm.Value
	if n.step != nil {
		stepVal = n.step.codegen()
		if stepVal.IsNil() {
			return llvm.ConstNull(varType)
		}
	} else {
		stepVal = llvm.ConstInt(varType, 1, true)
		if varType == ctx.DoubleType() {
			stepVal = llvm.ConstFloat(ctx.DoubleType(), 1)
		}
	}

	endVal := n.test.codegen()
	if endVal.IsNil() {
		return endVal
	}

	curVar := builder.CreateLoad(varType, alloca, n.counter)
	var nextVar llvm.Value
	if varType == ctx.DoubleType() {
		nextVar = builder.CreateFAdd(curVar, stepVal, "nextvar")
	} else {
		nextVar = builder.CreateAdd(curVar, stepVal, "nextvar")
	}
	builder.CreateStore(nextVar, alloca)

	// Condition: endVal != 0
	var cond llvm.Value
	if endVal.Type() == ctx.Int64Type() || endVal.Type() == ctx.Int1Type() {
		cond = builder.CreateICmp(llvm.IntNE, endVal, llvm.ConstInt(endVal.Type(), 0, false), "loopcond")
	} else {
		cond = builder.CreateFCmp(llvm.FloatONE, endVal, llvm.ConstFloat(ctx.DoubleType(), 0), "loopcond")
	}
	afterBlk := llvm.AddBasicBlock(parentFunc, "afterloop")
	builder.CreateCondBr(cond, loopBlk, afterBlk)
	builder.SetInsertPointAtEnd(afterBlk)

	if !oldVal.IsNil() {
		namedVals[n.counter] = oldVal
		namedTypes[n.counter] = oldType
	} else {
		delete(namedVals, n.counter)
		delete(namedTypes, n.counter)
	}

	if varType == ctx.DoubleType() {
		return llvm.ConstFloat(ctx.DoubleType(), 0)
	}
	return llvm.ConstInt(varType, 0, true)
}

func (n *unaryNode) codegen() llvm.Value {
	operandValue := n.operand.codegen()
	if operandValue.IsNil() {
		return ErrorV("nil operand")
	}

	f := rootModule.NamedFunction("unary" + n.name)
	if f.IsNil() {
		return ErrorV(fmt.Sprintf("unknown unary operator: %s", n.name))
	}
	fnType := f.GlobalValueType()
	// Auto-convert operand to expected param type.
	expectedParam := fnType.ParamTypes()[0]
	if operandValue.Type() != expectedParam {
		if expectedParam == ctx.DoubleType() && (operandValue.Type() == ctx.Int64Type() || operandValue.Type() == ctx.Int1Type()) {
			operandValue = builder.CreateSIToFP(operandValue, ctx.DoubleType(), "inttofp")
		} else if expectedParam == ctx.Int64Type() && operandValue.Type() == ctx.DoubleType() {
			operandValue = builder.CreateFPToSI(operandValue, ctx.Int64Type(), "fptosi")
		}
	}
	ftyp := llvm.FunctionType(fnType.ReturnType(), []llvm.Type{operandValue.Type()}, false)
	return builder.CreateCall(ftyp, f, []llvm.Value{operandValue}, "unop")
}

func (n *variableExprNode) codegen() llvm.Value {
	oldvars := make([]llvm.Value, len(n.vars))
	oldtypes := make([]llvm.Type, len(n.vars))

	f := builder.GetInsertBlock().Parent()
	for i := range n.vars {
		name := n.vars[i].name
		node := n.vars[i].node

		var val llvm.Value
		if node != nil {
			val = node.codegen()
			if val.IsNil() {
				return val
			}
		} else {
			val = llvm.ConstInt(ctx.Int64Type(), 0, true)
		}

		alloca := createEntryBlockAlloca(f, name, val.Type())
		builder.CreateStore(val, alloca)

		oldvars[i] = namedVals[name]
		oldtypes[i] = namedTypes[name]
		namedVals[name] = alloca
		namedTypes[name] = val.Type()
	}

	bodyVal := n.body.codegen()
	if bodyVal.IsNil() {
		return ErrorV("body returns nil")
	}

	for i := range n.vars {
		namedVals[n.vars[i].name] = oldvars[i]
		namedTypes[n.vars[i].name] = oldtypes[i]
	}

	return bodyVal
}

func (n *fnCallNode) codegen() llvm.Value {
	callee := rootModule.NamedFunction(n.callee)
	if callee.IsNil() {
		return ErrorV(fmt.Sprintf("unknown function referenced: %s", n.callee))
	}

	if callee.ParamsCount() != len(n.args) {
		return ErrorV(fmt.Sprintf("incorrect number of arguments passed to %s: expected %d, got %d",
			n.callee, callee.ParamsCount(), len(n.args)))
	}

	// With opaque pointers, use GlobalValueType() to get the function type.
	fnType := callee.GlobalValueType()

	args := make([]llvm.Value, len(n.args))
	argtyps := make([]llvm.Type, len(n.args))
	for i, arg := range n.args {
		args[i] = arg.codegen()
		if args[i].IsNil() {
			return ErrorV(fmt.Sprintf("argument %d to %s was nil", i, n.callee))
		}
		// Auto-convert int to double if the callee expects double.
		expectedType := fnType.ParamTypes()[i]
		actualType := args[i].Type()
		if actualType != expectedType {
			if expectedType == ctx.DoubleType() && (actualType == ctx.Int64Type() || actualType == ctx.Int1Type()) {
				args[i] = builder.CreateSIToFP(args[i], ctx.DoubleType(), "inttofp")
			} else if expectedType == ctx.Int64Type() && actualType == ctx.DoubleType() {
				args[i] = builder.CreateFPToSI(args[i], ctx.Int64Type(), "fptosi")
			}
		}
		argtyps[i] = args[i].Type()
	}

	retType := fnType.ReturnType()
	ftyp := llvm.FunctionType(retType, argtyps, false)
	return builder.CreateCall(ftyp, callee, args, "calltmp")
}

func (n *binaryNode) codegen() llvm.Value {
	if n.op == "=" {
		l, ok := n.left.(*variableNode)
		if !ok {
			return ErrorV("destination of '=' must be a variable")
		}

		val := n.right.codegen()
		if val.IsNil() {
			return ErrorV("cannot assign null value")
		}

		p := namedVals[l.name]
		if p.IsNil() {
			return ErrorV(fmt.Sprintf("undefined variable: %s", l.name))
		}
		builder.CreateStore(val, p)
		return val
	}

	l := n.left.codegen()
	r := n.right.codegen()
	if l.IsNil() || r.IsNil() {
		return ErrorV("operand was nil")
	}

	// Auto-promote: if one is int and other is double, convert int to double.
	if l.Type() != r.Type() {
		if l.Type() == ctx.DoubleType() && (r.Type() == ctx.Int64Type() || r.Type() == ctx.Int1Type()) {
			r = builder.CreateSIToFP(r, ctx.DoubleType(), "promote")
		} else if r.Type() == ctx.DoubleType() && (l.Type() == ctx.Int64Type() || l.Type() == ctx.Int1Type()) {
			l = builder.CreateSIToFP(l, ctx.DoubleType(), "promote")
		}
	}

	isFloat := l.Type() == ctx.DoubleType()

	switch n.op {
	case "+":
		if isFloat {
			return builder.CreateFAdd(l, r, "addtmp")
		}
		return builder.CreateAdd(l, r, "addtmp")
	case "-":
		if isFloat {
			return builder.CreateFSub(l, r, "subtmp")
		}
		return builder.CreateSub(l, r, "subtmp")
	case "*":
		if isFloat {
			return builder.CreateFMul(l, r, "multmp")
		}
		return builder.CreateMul(l, r, "multmp")
	case "/":
		if isFloat {
			return builder.CreateFDiv(l, r, "divtmp")
		}
		return builder.CreateSDiv(l, r, "divtmp")
	case "%":
		if isFloat {
			return builder.CreateFRem(l, r, "modtmp")
		}
		return builder.CreateSRem(l, r, "modtmp")
	case "<":
		if isFloat {
			l = builder.CreateFCmp(llvm.FloatOLT, l, r, "cmptmp")
		} else {
			l = builder.CreateICmp(llvm.IntSLT, l, r, "cmptmp")
		}
		return builder.CreateUIToFP(l, ctx.DoubleType(), "booltmp")
	case ">":
		if isFloat {
			l = builder.CreateFCmp(llvm.FloatOGT, l, r, "cmptmp")
		} else {
			l = builder.CreateICmp(llvm.IntSGT, l, r, "cmptmp")
		}
		return builder.CreateUIToFP(l, ctx.DoubleType(), "booltmp")
	default:
		function := rootModule.NamedFunction("binary" + n.op)
		if function.IsNil() {
			return ErrorV(fmt.Sprintf("invalid binary operator: %s", n.op))
		}
		fnType := function.GlobalValueType()
		retType := fnType.ReturnType()
		// Auto-convert operands to expected param types.
		for i, val := range []llvm.Value{l, r} {
			expected := fnType.ParamTypes()[i]
			if val.Type() != expected {
				if expected == ctx.DoubleType() && (val.Type() == ctx.Int64Type() || val.Type() == ctx.Int1Type()) {
					val = builder.CreateSIToFP(val, ctx.DoubleType(), "inttofp")
				} else if expected == ctx.Int64Type() && val.Type() == ctx.DoubleType() {
					val = builder.CreateFPToSI(val, ctx.Int64Type(), "fptosi")
				}
			}
			if i == 0 {
				l = val
			} else {
				r = val
			}
		}
		paramTypes := []llvm.Type{l.Type(), r.Type()}
		ftyp := llvm.FunctionType(retType, paramTypes, false)
		return builder.CreateCall(ftyp, function, []llvm.Value{l, r}, "binop")
	}
}

func (n *fnPrototypeNode) codegen() llvm.Value {
	funcArgs := make([]llvm.Type, len(n.args))
	for i := range n.args {
		if i < len(n.paramTypes) {
			if n.paramTypes[i] == TypeStruct {
				// Struct receiver: pass as pointer to struct type.
				if st, ok := llvmStructTypes[n.receiverType]; ok {
					funcArgs[i] = llvm.PointerType(st, 0)
				} else {
					funcArgs[i] = llvm.PointerType(ctx.StructType([]llvm.Type{}, false), 0)
				}
			} else {
				funcArgs[i] = llvmTypeFor(n.paramTypes[i])
			}
		} else {
			funcArgs[i] = ctx.Int64Type()
		}
	}

	retType := llvmTypeFor(n.retType)
	funcType := llvm.FunctionType(retType, funcArgs, false)

	// Encode method names as TypeName.MethodName.
	llvmName := n.name
	if n.receiverType != "" {
		llvmName = n.receiverType + "." + n.name
	}

	function := llvm.AddFunction(rootModule, llvmName, funcType)

	if function.Name() != llvmName {
		function.EraseFromParentAsFunction()
		function = rootModule.NamedFunction(llvmName)
	}

	if function.BasicBlocksCount() != 0 {
		return ErrorV(fmt.Sprintf("redefinition of function: %s", llvmName))
	}

	if function.ParamsCount() != len(n.args) {
		return ErrorV(fmt.Sprintf("redefinition of function with different number of args: %s", llvmName))
	}

	for i, param := range function.Params() {
		param.SetName(n.args[i])
		namedVals[n.args[i]] = param
		if i < len(n.paramTypes) {
			if n.paramTypes[i] == TypeStruct && n.receiverType != "" {
				// Struct receiver: actual type is pointer to struct.
				if st, ok := llvmStructTypes[n.receiverType]; ok {
					namedTypes[n.args[i]] = llvm.PointerType(st, 0)
				} else {
					namedTypes[n.args[i]] = ctx.VoidType()
				}
			} else {
				namedTypes[n.args[i]] = llvmTypeFor(n.paramTypes[i])
			}
		} else {
			namedTypes[n.args[i]] = ctx.Int64Type()
		}
	}

	return function
}

// Stub codegen methods for new AST nodes (will be implemented in Task 5).

func (n *boolNode) codegen() llvm.Value {
	if n.val {
		return llvm.ConstInt(ctx.Int64Type(), 1, false)
	}
	return llvm.ConstInt(ctx.Int64Type(), 0, false)
}

func (n *nilNode) codegen() llvm.Value {
	return llvm.ConstNull(ctx.Int64Type())
}

func (n *selfNode) codegen() llvm.Value {
	v := namedVals["$"]
	if v.IsNil() {
		return ErrorV("$ used outside of method")
	}
	elemType := namedTypes["$"]
	if elemType.IsNil() {
		return ErrorV("unknown type for $")
	}
	return builder.CreateLoad(elemType, v, "$")
}

// resolveStructType returns the struct type name and LLVM type for an object node.
// Works with opaque pointers by looking up type info from namedTypes or selfNode.structName.
func resolveStructType(obj node) (string, llvm.Type, *StructDef) {
	switch o := obj.(type) {
	case *selfNode:
		if o.structName != "" {
			if st, ok := llvmStructTypes[o.structName]; ok {
				return o.structName, st, lookupStruct(o.structName)
			}
		}
	case *variableNode:
		if t, ok := namedTypes[o.name]; ok {
			for name, st := range llvmStructTypes {
				if st == t {
					return name, st, lookupStruct(name)
				}
			}
		}
	}
	return "", llvm.Type{}, nil
}

func (n *fieldAccessNode) codegen() llvm.Value {
	obj := n.object.codegen()
	if obj.IsNil() {
		return ErrorV("field access: object codegen failed")
	}

	typeName, llvmStructType, structDef := resolveStructType(n.object)
	if typeName == "" || structDef == nil {
		return ErrorV(fmt.Sprintf("field access .%s: cannot determine struct type", n.field))
	}

	idx := structDef.fieldIndex(n.field)
	if idx < 0 {
		return ErrorV(fmt.Sprintf("unknown field: %s", n.field))
	}

	fieldType := llvmTypeFor(structDef.Fields[idx].Type)

	// obj must be a pointer to struct for GEP.
	if obj.Type().TypeKind() != llvm.PointerTypeKind {
		return ErrorV("field access requires a pointer to struct")
	}

	indices := []llvm.Value{
		llvm.ConstInt(ctx.Int32Type(), 0, false),
		llvm.ConstInt(ctx.Int32Type(), uint64(idx), false),
	}
	fieldPtr := builder.CreateInBoundsGEP(llvmStructType, obj, indices, n.field)
	return builder.CreateLoad(fieldType, fieldPtr, n.field)
}

func (n *methodCallNode) codegen() llvm.Value {
	obj := n.object.codegen()
	if obj.IsNil() {
		return ErrorV("method call: object codegen failed")
	}

	typeName, _, _ := resolveStructType(n.object)
	if typeName == "" {
		return ErrorV("method call on non-struct value")
	}

	// Look up the method function.
	funcName := typeName + "." + n.method
	callee := rootModule.NamedFunction(funcName)
	if callee.IsNil() {
		return ErrorV(fmt.Sprintf("unknown method: %s.%s", typeName, n.method))
	}

	// Build args: receiver + explicit args.
	args := make([]llvm.Value, 0, 1+len(n.args))
	argTypes := make([]llvm.Type, 0, 1+len(n.args))

	// Pass receiver as pointer to struct.
	if obj.Type().TypeKind() != llvm.PointerTypeKind {
		return ErrorV("method receiver must be a pointer")
	}
	args = append(args, obj)
	argTypes = append(argTypes, obj.Type())

	for i, arg := range n.args {
		v := arg.codegen()
		if v.IsNil() {
			return ErrorV(fmt.Sprintf("argument %d was nil", i))
		}
		args = append(args, v)
		argTypes = append(argTypes, v.Type())
	}

	retType := callee.GlobalValueType().ReturnType()
	ftyp := llvm.FunctionType(retType, argTypes, false)
	return builder.CreateCall(ftyp, callee, args, "methodtmp")
}

func (n *structDefNode) codegen() llvm.Value {
	// Create the LLVM struct type from the field definitions.
	fieldTypes := make([]llvm.Type, len(n.fields))
	for i, f := range n.fields {
		fieldTypes[i] = llvmTypeFor(f.Type)
	}
	structType := ctx.StructCreateNamed(n.name)
	structType.StructSetBody(fieldTypes, false)
	llvmStructTypes[n.name] = structType
	// Return nil — the type is registered as a side effect.
	// Parent functionNode.codegen() returns nil to skip this node.
	return llvm.Value{}
}

func (n *structLitNode) codegen() llvm.Value {
	return ErrorV("struct literal codegen not yet implemented")
}

func (n *blockNode) codegen() llvm.Value {
	var last llvm.Value
	for _, s := range n.stmts {
		v := s.codegen()
		if v.IsNil() {
			return v
		}
		last = v
	}
	return last
}

func (n *functionNode) codegen() llvm.Value {
	namedVals = make(map[string]llvm.Value)
	namedTypes = make(map[string]llvm.Type)
	p, ok := n.proto.(*fnPrototypeNode)
	if !ok {
		return ErrorV("invalid prototype")
	}

	// Struct definitions are handled by structDefNode.codegen.
	// They register the LLVM type as a side effect.
	// Return nil so visitors skip this node (no function is created).
	if _, isStruct := n.body.(*structDefNode); isStruct {
		n.body.codegen()
		return llvm.Value{}
	}

	theFunction := n.proto.codegen()
	if theFunction.IsNil() {
		return ErrorV("prototype missing")
	}

	block := llvm.AddBasicBlock(theFunction, "entry")
	builder.SetInsertPointAtEnd(block)

	p.createArgAlloca(theFunction)

	retVal := n.body.codegen()
	if retVal.IsNil() {
		theFunction.EraseFromParentAsFunction()
		return ErrorV("function body codegen failed")
	}

	retType := llvmTypeFor(p.retType)

	// Auto-convert return value to match declared return type.
	if retVal.Type() != retType && p.retType != TypeVoid {
		if retType == ctx.DoubleType() && (retVal.Type() == ctx.Int64Type() || retVal.Type() == ctx.Int1Type()) {
			retVal = builder.CreateSIToFP(retVal, ctx.DoubleType(), "retconv")
		} else if retType == ctx.Int64Type() && retVal.Type() == ctx.DoubleType() {
			retVal = builder.CreateFPToSI(retVal, ctx.Int64Type(), "retconv")
		} else if retType == ctx.Int1Type() {
			retVal = builder.CreateICmp(llvm.IntNE, retVal, llvm.ConstNull(retVal.Type()), "retconv")
		}
	}

	if p.retType == TypeVoid {
		builder.CreateRetVoid()
	} else {
		builder.CreateRet(retVal)
	}
	if llvm.VerifyFunction(theFunction, llvm.PrintMessageAction) != nil {
		theFunction.EraseFromParentAsFunction()
		return ErrorV(fmt.Sprintf("function verification failed: %s", p.name))
	}

	slog.Debug("compiled function", "name", p.name)
	return theFunction
}
