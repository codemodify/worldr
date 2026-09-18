#ifndef WORLDR_XWM_H
#define WORLDR_XWM_H
#include <stdint.h>
#define WORLDR_XWM_WINDOWS 64
#define WORLDR_XWM_CLIPBOARD_MIMES 64
#define WORLDR_XWM_CLIPBOARD_MIME_SIZE 256
typedef struct worldr_xwm worldr_xwm;
typedef struct {
  uint32_t id, object_id, transient_for, pid;
  int x, y, width, height, override_redirect, mapped;
  char title[512], app_id[256];
} worldr_xwm_window;
typedef struct {
  uint64_t revision, id, external_id;
  int mime_count;
  char mimes[WORLDR_XWM_CLIPBOARD_MIMES][WORLDR_XWM_CLIPBOARD_MIME_SIZE];
} worldr_xwm_clipboard_offer;
typedef struct {
  uint64_t id, external_id;
  char mime[WORLDR_XWM_CLIPBOARD_MIME_SIZE];
} worldr_xwm_clipboard_request;
typedef struct {
  uint64_t revision, id;
  uint32_t owner, pid, target;
  int active, accepted, dropped, mime_count;
  char mimes[WORLDR_XWM_CLIPBOARD_MIMES][WORLDR_XWM_CLIPBOARD_MIME_SIZE];
} worldr_xwm_dnd_offer;
// Owns fd even on failure. The owner thread exclusively calls all XCB methods.
worldr_xwm *worldr_xwm_open(int fd, char *error, int capacity);
void worldr_xwm_close(worldr_xwm *);
int worldr_xwm_poll(worldr_xwm *, worldr_xwm_window *, int capacity);
void worldr_xwm_focus(worldr_xwm *, uint32_t);
void worldr_xwm_resize(worldr_xwm *, uint32_t, int, int);
void worldr_xwm_delete(worldr_xwm *, uint32_t);
void worldr_xwm_get_clipboard_offer(worldr_xwm *, worldr_xwm_clipboard_offer *);
int worldr_xwm_clipboard_publish(worldr_xwm *, uint64_t, const char **, int);
int worldr_xwm_clipboard_receive(worldr_xwm *, uint64_t, uint64_t,
                                 const char *);
void worldr_xwm_clipboard_cancel(worldr_xwm *, uint64_t);
int worldr_xwm_clipboard_result(worldr_xwm *, uint64_t *, int *, uint8_t **,
                                uint32_t *);
void worldr_xwm_clipboard_consume_result(worldr_xwm *);
int worldr_xwm_clipboard_requests(worldr_xwm *, worldr_xwm_clipboard_request *,
                                  int);
void worldr_xwm_clipboard_reply(worldr_xwm *, uint64_t, const uint8_t *,
                                uint32_t, int);
void worldr_xwm_get_dnd_offer(worldr_xwm *, worldr_xwm_dnd_offer *);
int worldr_xwm_dnd_motion(worldr_xwm *, uint32_t source, uint32_t target,
                          int x, int y, uint32_t time);
int worldr_xwm_dnd_drop(worldr_xwm *, uint32_t source, uint32_t target,
                        uint32_t time);
void worldr_xwm_dnd_cancel(worldr_xwm *);
#endif
