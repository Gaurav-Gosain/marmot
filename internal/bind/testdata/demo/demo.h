#ifndef DEMO_H
#define DEMO_H
#include <stddef.h>

/* Version of the library. */
#define DEMO_VERSION "1.2"
#define DEMO_MAX_NAME 64
#define DEMO_FLAG_A 0x1
#define DEMO_FLAG_B (DEMO_FLAG_A << 1)
#define DEMO_OK 0
#define DEMO_ERR (-1)

/** An opaque database handle. */
typedef struct demo_db demo_db;

/** An opaque file handle. */
typedef struct demo_file_s *demo_file;

/** A point, passed by pointer. */
typedef struct demo_point { int x; int y; } demo_point;

/** Colors a thing can have. */
typedef enum demo_color { DEMO_RED, DEMO_GREEN = 5, DEMO_BLUE } demo_color;

/** Opens a database at path. Returns DEMO_OK or an error. */
int demo_open(const char *path, demo_db **out);
/** Closes the database. */
int demo_close(demo_db *db);
/** Puts a value; returns a status. */
int demo_put(demo_db *db, const char *key, const void *data, size_t len);
/** The number of rows. */
long demo_count(demo_db *db);
/** The last error, owned by the database. */
const char *demo_errmsg(demo_db *db);
/** A copy of the name; free it with demo_free. */
char *demo_name(demo_db *db);
void demo_free(void *p);
/** The color of the database. */
demo_color demo_db_color(demo_db *db);
/** Moves a point. */
void demo_move(demo_point *p, int dx, int dy);
/** Opens a file. */
demo_file demo_fopen(const char *name);
int demo_fclose(demo_file f);
/** Logs a message. */
int demo_log(const char *fmt, ...);
/** Reads the size into an out parameter. */
int demo_size(demo_db *db, int *size);
/** Unclear: who frees this? */
char *demo_unowned(void);
#endif
