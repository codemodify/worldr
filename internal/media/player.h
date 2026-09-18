#ifndef WORLDR_MEDIA_PLAYER_H
#define WORLDR_MEDIA_PLAYER_H

#include <mpv/client.h>
#include <mpv/render.h>
#include <stddef.h>
#include <stdint.h>

typedef struct worldr_media worldr_media;
typedef struct worldr_media_event {
    int id, error, property, present, end_reason;
    double value;
} worldr_media_event;

worldr_media *worldr_media_new(const char *audio_output, int paused, double volume, int muted, int *error, const char **stage);
void worldr_media_free(worldr_media *player);
int worldr_media_load(worldr_media *player, const char *path);
int worldr_media_pause(worldr_media *player, int pause);
int worldr_media_seek(worldr_media *player, const char *seconds);
int worldr_media_volume(worldr_media *player, double volume);
int worldr_media_mute(worldr_media *player, int mute);
worldr_media_event worldr_media_poll(worldr_media *player);
int worldr_media_render(worldr_media *player, uint8_t *pixels, int width, int height, size_t stride, int force);

#endif
