package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"unsafe"

	"tinygo.org/x/go-llvm"
)

func VisitNodes(roots <-chan node, action func(node, llvm.Value)) {
	for nod := range roots {
		val := nod.codegen()
		if val.IsNil() {
			slog.Warn("codegen failed for node, skipping", "node", fmt.Sprintf("%v", nod))
			continue
		}
		if action != nil {
			action(nod, val)
		}
	}
}

func Compile(roots <-chan node, module llvm.Module) ([]byte, error) {
	VisitNodes(roots, nil)
	if *optimize > 0 {
		Optimize()
	}
	buffer, err := machine.EmitToMemoryBuffer(module, llvm.ObjectFile)
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func EmitIR(roots <-chan node) {
	fmt.Fprintln(os.Stdout, "; ModuleID = 'deer'")
	VisitNodes(roots, func(nod node, val llvm.Value) {
		val.Dump()
	})
}

func Exec(roots <-chan node) {
	// First pass: codegen all nodes so MCJIT sees the complete module.
	type entry struct {
		nod node
		val llvm.Value
	}
	var entries []entry
	for nod := range roots {
		val := nod.codegen()
		if val.IsNil() {
			slog.Warn("codegen failed for node, skipping", "node", fmt.Sprintf("%v", nod))
			continue
		}
		entries = append(entries, entry{nod, val})
	}
	// Second pass: run top-level expressions now that the module is complete.
	for _, e := range entries {
		if isTopLevelExpr(e.nod) {
			_, name := getFuncName(e.nod)
			addr := execEngine.GetFunctionAddress(name)
			if addr == 0 {
				slog.Error("failed to get function address", "name", name)
				continue
			}
			fnPtr := unsafe.Pointer(uintptr(addr))
			// Check return type to call the right native function.
			fn := rootModule.NamedFunction(name)
			retType := fn.GlobalValueType().ReturnType()
			if retType == ctx.Int64Type() {
				result := callNativeFuncInt(fnPtr)
				slog.Debug("evaluated expression", "result", result)
			} else {
				result := callNativeFunc(fnPtr)
				slog.Debug("evaluated expression", "result", result)
			}
		}
	}
}

func getFuncName(n node) (bool, string) {
	if n.Kind() != nodeFunction {
		return false, ""
	}
	fn, ok := n.(*functionNode)
	if !ok {
		return false, ""
	}
	p, ok := fn.proto.(*fnPrototypeNode)
	if !ok {
		return false, ""
	}
	return true, p.name
}

func isTopLevelExpr(n node) bool {
	isFunc, name := getFuncName(n)
	return isFunc && (strings.HasPrefix(name, "__anon") || name == "main")
}
