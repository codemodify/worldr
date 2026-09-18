#ifndef WORLDR_SEAT_H
#define WORLDR_SEAT_H
#include <stdint.h>
typedef struct worldr_seat worldr_seat;
typedef struct worldr_input worldr_input;
typedef struct {
  int kind;
  double x, y;
  uint32_t code, time;
  int pressed;
} worldr_input_event;
worldr_seat *worldr_seat_open(int *error);
const char *worldr_seat_name(worldr_seat *);
int worldr_seat_dispatch(worldr_seat *);
int worldr_seat_device_open(worldr_seat *, const char *, int *fd);
int worldr_seat_device_close(worldr_seat *, int);
int worldr_seat_disable(worldr_seat *);
int worldr_seat_switch(worldr_seat *, int);
int worldr_seat_close(worldr_seat *);
worldr_input *worldr_input_open(worldr_seat *, int *error);
int worldr_input_poll(worldr_input *, worldr_input_event *, int capacity);
int worldr_input_close(worldr_input *);
#endif
