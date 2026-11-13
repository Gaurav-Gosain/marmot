#ifndef CLIB_H
#define CLIB_H
#include <stddef.h>

typedef struct {
	double sum;
	long count;
} stats;

/* Defined in clib.c. */
void stats_add(stats *s, double v);
double stats_mean(const stats *s);
size_t clib_reverse(char *s);
long clib_scaled(long v);

/* Defined in asm.S. */
long clib_asm_add(long a, long b);
#endif
