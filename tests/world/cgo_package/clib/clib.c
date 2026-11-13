#include <string.h>
#include "clib.h"

void stats_add(stats *s, double v) {
	s->sum += v;
	s->count++;
}

double stats_mean(const stats *s) { return s->count ? s->sum / s->count : 0; }

size_t clib_reverse(char *s) {
	size_t n = strlen(s);
	for (size_t i = 0; i < n / 2; i++) {
		char t = s[i];
		s[i] = s[n - 1 - i];
		s[n - 1 - i] = t;
	}
	return n;
}

/* SCALE comes from #cgo CFLAGS in clib.go. */
long clib_scaled(long v) { return v * SCALE; }
