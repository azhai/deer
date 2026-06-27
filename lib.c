#include <stdio.h>
#include <math.h>
#include <stdint.h>

double printd(double x) {
	printf("%g\n", x);
	fflush(stdout);
	return x;
}

double printi(double x) {
	printf("%d\n", (int)x);
	fflush(stdout);
	return x;
}

double putchard(double x) {
	putchar((char)x);
	fflush(stdout);
	return x;
}

double println(double x) {
	printf("result: %0.6f\n", x);
	fflush(stdout);
	return x;
}

/* Print a 64-bit integer */
int64_t print_int(int64_t x) {
	printf("%ld\n", (long)x);
	fflush(stdout);
	return x;
}

/* Print a boolean value */
int64_t print_bool(int64_t x) {
	printf(x ? "true\n" : "false\n");
	fflush(stdout);
	return x;
}

double acos_(double x) { return acos(x); }
double asin_(double x) { return asin(x); }
double atan_(double x) { return atan(x); }
double atan2_(double x, double y) { return atan2(x, y); }
double ceil_(double x) { return ceil(x); }
double cos_(double x) { return cos(x); }
double cosh_(double x) { return cosh(x); }
double exp_(double x) { return exp(x); }
double exp2_(double x) { return exp2(x); }
double floor_(double x) { return floor(x); }
double fmod_(double x, double y) { return fmod(x, y); }
double log_(double x) { return log(x); }
double log2_(double x) { return log2(x); }
double log10_(double x) { return log10(x); }
double pow_(double x, double y) { return pow(x, y); }
double sin_(double x) { return sin(x); }
double sinh_(double x) { return sinh(x); }
double sqrt_(double x) { return sqrt(x); }
double tan_(double x) { return tan(x); }
double tanh_(double x) { return tanh(x); }

/* Address getters for JIT symbol registration. */
void* get_printd_addr(void) { return (void*)printd; }
void* get_printi_addr(void) { return (void*)printi; }
void* get_putchard_addr(void) { return (void*)putchard; }
void* get_println_addr(void) { return (void*)println; }
void* get_print_int_addr(void) { return (void*)print_int; }
void* get_print_bool_addr(void) { return (void*)print_bool; }
void* get_acos__addr(void) { return (void*)acos_; }
void* get_asin__addr(void) { return (void*)asin_; }
void* get_atan__addr(void) { return (void*)atan_; }
void* get_atan2__addr(void) { return (void*)atan2_; }
void* get_ceil__addr(void) { return (void*)ceil_; }
void* get_cos__addr(void) { return (void*)cos_; }
void* get_cosh__addr(void) { return (void*)cosh_; }
void* get_exp__addr(void) { return (void*)exp_; }
void* get_exp2__addr(void) { return (void*)exp2_; }
void* get_floor__addr(void) { return (void*)floor_; }
void* get_fmod__addr(void) { return (void*)fmod_; }
void* get_log__addr(void) { return (void*)log_; }
void* get_log2__addr(void) { return (void*)log2_; }
void* get_log10__addr(void) { return (void*)log10_; }
void* get_pow__addr(void) { return (void*)pow_; }
void* get_sin__addr(void) { return (void*)sin_; }
void* get_sinh__addr(void) { return (void*)sinh_; }
void* get_sqrt__addr(void) { return (void*)sqrt_; }
void* get_tan__addr(void) { return (void*)tan_; }
void* get_tanh__addr(void) { return (void*)tanh_; }

/* Call a no-arg function pointer that returns double. */
double call_fn_ptr(void* fn) {
	return ((double (*)())fn)();
}

/* Call a no-arg function pointer that returns int64. */
int64_t call_fn_int_ptr(void* fn) {
	return ((int64_t (*)())fn)();
}
