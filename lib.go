package main

/*
#include <stdlib.h>

// Forward-declare LLVMAddSymbol so the JIT can resolve host functions.
extern void LLVMAddSymbol(const char *name, void *addr);

// All runtime functions are defined in lib.c (also used for native linking).
extern double printd(double x);
extern double printi(double x);
extern double putchard(double x);
extern double println(double x);
extern double acos_(double x);
extern double asin_(double x);
extern double atan_(double x);
extern double atan2_(double x, double y);
extern double ceil_(double x);
extern double cos_(double x);
extern double cosh_(double x);
extern double exp_(double x);
extern double exp2_(double x);
extern double floor_(double x);
extern double fmod_(double x, double y);
extern double log_(double x);
extern double log2_(double x);
extern double log10_(double x);
extern double pow_(double x, double y);
extern double sin_(double x);
extern double sinh_(double x);
extern double sqrt_(double x);
extern double tan_(double x);
extern double tanh_(double x);

extern void* get_printd_addr(void);
extern void* get_printi_addr(void);
extern void* get_putchard_addr(void);
extern void* get_println_addr(void);
extern void* get_acos__addr(void);
extern void* get_asin__addr(void);
extern void* get_atan__addr(void);
extern void* get_atan2__addr(void);
extern void* get_ceil__addr(void);
extern void* get_cos__addr(void);
extern void* get_cosh__addr(void);
extern void* get_exp__addr(void);
extern void* get_exp2__addr(void);
extern void* get_floor__addr(void);
extern void* get_fmod__addr(void);
extern void* get_log__addr(void);
extern void* get_log2__addr(void);
extern void* get_log10__addr(void);
extern void* get_pow__addr(void);
extern void* get_sin__addr(void);
extern void* get_sinh__addr(void);
extern void* get_sqrt__addr(void);
extern void* get_tan__addr(void);
extern void* get_tanh__addr(void);

extern double call_fn_ptr(void* fn);

// llvm_add_symbol wraps LLVMAddSymbol for Go to call.
static void llvm_add_symbol(const char* name, void* addr) {
	LLVMAddSymbol(name, addr);
}
*/
import "C"

import "unsafe"

// hostSymbolAddrs returns the host addresses of the C functions so the
// JIT engine can resolve calls to them.
func hostSymbolAddrs() map[string]unsafe.Pointer {
	return map[string]unsafe.Pointer{
		"printd":   unsafe.Pointer(C.get_printd_addr()),
		"printi":   unsafe.Pointer(C.get_printi_addr()),
		"putchard": unsafe.Pointer(C.get_putchard_addr()),
		"println":  unsafe.Pointer(C.get_println_addr()),
		"acos_":    unsafe.Pointer(C.get_acos__addr()),
		"asin_":    unsafe.Pointer(C.get_asin__addr()),
		"atan_":    unsafe.Pointer(C.get_atan__addr()),
		"atan2_":   unsafe.Pointer(C.get_atan2__addr()),
		"ceil_":    unsafe.Pointer(C.get_ceil__addr()),
		"cos_":     unsafe.Pointer(C.get_cos__addr()),
		"cosh_":    unsafe.Pointer(C.get_cosh__addr()),
		"exp_":     unsafe.Pointer(C.get_exp__addr()),
		"exp2_":    unsafe.Pointer(C.get_exp2__addr()),
		"floor_":   unsafe.Pointer(C.get_floor__addr()),
		"fmod_":    unsafe.Pointer(C.get_fmod__addr()),
		"log_":     unsafe.Pointer(C.get_log__addr()),
		"log2_":    unsafe.Pointer(C.get_log2__addr()),
		"log10_":   unsafe.Pointer(C.get_log10__addr()),
		"pow_":     unsafe.Pointer(C.get_pow__addr()),
		"sin_":     unsafe.Pointer(C.get_sin__addr()),
		"sinh_":    unsafe.Pointer(C.get_sinh__addr()),
		"sqrt_":    unsafe.Pointer(C.get_sqrt__addr()),
		"tan_":     unsafe.Pointer(C.get_tan__addr()),
		"tanh_":    unsafe.Pointer(C.get_tanh__addr()),
	}
}

// callNativeFunc invokes a no-arg native function pointer that returns double.
func callNativeFunc(fnPtr unsafe.Pointer) float64 {
	return float64(C.call_fn_ptr(fnPtr))
}

// registerLLVMSymbol registers a host symbol with LLVM's global symbol table
// so the JIT can resolve calls to it.
func registerLLVMSymbol(name string, addr unsafe.Pointer) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	C.llvm_add_symbol(cname, addr)
}
