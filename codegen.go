package main

import (
	"fmt"
	"log/slog"
	"os"

	"tinygo.org/x/go-llvm"
)

var (
	ctx        = llvm.NewContext()
	builder    = ctx.NewBuilder()
	rootModule = ctx.NewModule("root")
	options    = llvm.NewMCJITCompilerOptions()
	namedVals  = map[string]llvm.Value{}
	execEngine llvm.ExecutionEngine
	machine    llvm.TargetMachine
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

func createEntryBlockAlloca(f llvm.Value, name string) llvm.Value {
	tmpB := ctx.NewBuilder()
	tmpB.SetInsertPoint(f.EntryBasicBlock(), f.EntryBasicBlock().FirstInstruction())
	return tmpB.CreateAlloca(ctx.DoubleType(), name)
}

func (n *fnPrototypeNode) createArgAlloca(f llvm.Value) {
	args := f.Params()
	for i := range args {
		alloca := createEntryBlockAlloca(f, n.args[i])
		builder.CreateStore(args[i], alloca)
		namedVals[n.args[i]] = alloca
	}
}

func (n *numberNode) codegen() llvm.Value {
	return llvm.ConstFloat(ctx.DoubleType(), n.val)
}

func (n *variableNode) codegen() llvm.Value {
	v := namedVals[n.name]
	if v.IsNil() {
		return ErrorV(fmt.Sprintf("unknown variable name: %s at %s", n.name, n.SrcPos))
	}
	return builder.CreateLoad(ctx.DoubleType(), v, n.name)
}

func (n *ifNode) codegen() llvm.Value {
	ifv := n.ifN.codegen()
	if ifv.IsNil() {
		return ErrorV("code generation failed for if condition")
	}
	ifv = builder.CreateFCmp(llvm.FloatONE, ifv, llvm.ConstFloat(ctx.DoubleType(), 0), "ifcond")

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
	phiNode := builder.CreatePHI(ctx.DoubleType(), "iftmp")
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
	alloca := createEntryBlockAlloca(parentFunc, n.counter)
	builder.CreateStore(startVal, alloca)
	loopBlk := llvm.AddBasicBlock(parentFunc, "loop")

	builder.CreateBr(loopBlk)
	builder.SetInsertPointAtEnd(loopBlk)

	oldVal := namedVals[n.counter]
	namedVals[n.counter] = alloca

	if n.body.codegen().IsNil() {
		return ErrorV("code generation failed for body expression")
	}

	var stepVal llvm.Value
	if n.step != nil {
		stepVal = n.step.codegen()
		if stepVal.IsNil() {
			return llvm.ConstNull(ctx.DoubleType())
		}
	} else {
		stepVal = llvm.ConstFloat(ctx.DoubleType(), 1)
	}

	endVal := n.test.codegen()
	if endVal.IsNil() {
		return endVal
	}

	curVar := builder.CreateLoad(ctx.DoubleType(), alloca, n.counter)
	nextVar := builder.CreateFAdd(curVar, stepVal, "nextvar")
	builder.CreateStore(nextVar, alloca)

	endVal = builder.CreateFCmp(llvm.FloatONE, endVal, llvm.ConstFloat(ctx.DoubleType(), 0), "loopcond")
	afterBlk := llvm.AddBasicBlock(parentFunc, "afterloop")
	builder.CreateCondBr(endVal, loopBlk, afterBlk)
	builder.SetInsertPointAtEnd(afterBlk)

	if !oldVal.IsNil() {
		namedVals[n.counter] = oldVal
	} else {
		delete(namedVals, n.counter)
	}

	return llvm.ConstFloat(ctx.DoubleType(), 0)
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
	ftyp := llvm.FunctionType(ctx.DoubleType(), []llvm.Type{ctx.DoubleType()}, false)
	return builder.CreateCall(ftyp, f, []llvm.Value{operandValue}, "unop")
}

func (n *variableExprNode) codegen() llvm.Value {
	oldvars := make([]llvm.Value, len(n.vars))

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
			val = llvm.ConstFloat(ctx.DoubleType(), 0)
		}

		alloca := createEntryBlockAlloca(f, name)
		builder.CreateStore(val, alloca)

		oldvars[i] = namedVals[name]
		namedVals[name] = alloca
	}

	bodyVal := n.body.codegen()
	if bodyVal.IsNil() {
		return ErrorV("body returns nil")
	}

	for i := range n.vars {
		namedVals[n.vars[i].name] = oldvars[i]
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

	args := make([]llvm.Value, len(n.args))
	argtyps := make([]llvm.Type, len(n.args))
	for i, arg := range n.args {
		args[i] = arg.codegen()
		argtyps[i] = ctx.DoubleType()
		if args[i].IsNil() {
			return ErrorV(fmt.Sprintf("argument %d to %s was nil", i, n.callee))
		}
	}

	ftyp := llvm.FunctionType(ctx.DoubleType(), argtyps, false)
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

	switch n.op {
	case "+":
		return builder.CreateFAdd(l, r, "addtmp")
	case "-":
		return builder.CreateFSub(l, r, "subtmp")
	case "*":
		return builder.CreateFMul(l, r, "multmp")
	case "/":
		return builder.CreateFDiv(l, r, "divtmp")
	case "%":
		return builder.CreateFRem(l, r, "modtmp")
	case "<":
		l = builder.CreateFCmp(llvm.FloatOLT, l, r, "cmptmp")
		return builder.CreateUIToFP(l, ctx.DoubleType(), "booltmp")
	case ">":
		l = builder.CreateFCmp(llvm.FloatOGT, l, r, "cmptmp")
		return builder.CreateUIToFP(l, ctx.DoubleType(), "booltmp")
	default:
		function := rootModule.NamedFunction("binary" + n.op)
		if function.IsNil() {
			return ErrorV(fmt.Sprintf("invalid binary operator: %s", n.op))
		}
		ftyp := llvm.FunctionType(ctx.DoubleType(), []llvm.Type{ctx.DoubleType(), ctx.DoubleType()}, false)
		return builder.CreateCall(ftyp, function, []llvm.Value{l, r}, "binop")
	}
}

func (n *fnPrototypeNode) codegen() llvm.Value {
	funcArgs := make([]llvm.Type, len(n.args))
	for i := range n.args {
		funcArgs[i] = ctx.DoubleType()
	}
	funcType := llvm.FunctionType(ctx.DoubleType(), funcArgs, false)
	function := llvm.AddFunction(rootModule, n.name, funcType)

	if function.Name() != n.name {
		function.EraseFromParentAsFunction()
		function = rootModule.NamedFunction(n.name)
	}

	if function.BasicBlocksCount() != 0 {
		return ErrorV(fmt.Sprintf("redefinition of function: %s", n.name))
	}

	if function.ParamsCount() != len(n.args) {
		return ErrorV(fmt.Sprintf("redefinition of function with different number of args: %s", n.name))
	}

	for i, param := range function.Params() {
		param.SetName(n.args[i])
		namedVals[n.args[i]] = param
	}

	return function
}

func (n *functionNode) codegen() llvm.Value {
	namedVals = make(map[string]llvm.Value)
	p, ok := n.proto.(*fnPrototypeNode)
	if !ok {
		return ErrorV("invalid prototype")
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

	builder.CreateRet(retVal)
	if llvm.VerifyFunction(theFunction, llvm.PrintMessageAction) != nil {
		theFunction.EraseFromParentAsFunction()
		return ErrorV(fmt.Sprintf("function verification failed: %s", p.name))
	}

	slog.Debug("compiled function", "name", p.name)
	return theFunction
}
