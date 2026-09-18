//go:build linux && cgo

#include "player.h"
#include <stdatomic.h>
#include <stdlib.h>

struct worldr_media {
    mpv_handle *handle;
    mpv_render_context *render;
    atomic_int redraw;
};

static void frame_ready(void *opaque) {
    worldr_media *player = opaque;
    atomic_store_explicit(&player->redraw, 1, memory_order_relaxed);
}

worldr_media *worldr_media_new(const char *audio_output, int paused, double volume, int muted, int *error, const char **stage) {
    *error = MPV_ERROR_NOMEM;
    *stage = "create player";
    worldr_media *player = calloc(1, sizeof(*player));
    if (!player) return NULL;
    atomic_init(&player->redraw, 1);
    player->handle = mpv_create();
    if (!player->handle) goto fail;
    // The host owns every interaction. No user config, scripts, IPC, terminal
    // input, subtitle discovery, references, network streams or extra windows.
    const char *options[][2] = {
        {"config", "no"}, {"load-scripts", "no"}, {"ytdl", "no"},
        {"osc", "no"}, {"input-default-bindings", "no"},
        {"input-vo-keyboard", "no"}, {"input-terminal", "no"},
        {"terminal", "no"}, {"msg-level", "all=no"}, {"osd-level", "0"},
        {"autoload-files", "no"}, {"sub-auto", "no"}, {"audio-file-auto", "no"},
        {"access-references", "no"}, {"load-unsafe-playlists", "no"},
        {"demuxer", "lavf"}, {"network-timeout", "1"},
        {"demuxer-max-bytes", "33554432"}, {"demuxer-max-back-bytes", "4194304"},
        {"cache", "no"}, {"vd-lavc-threads", "4"}, {"hwdec", "no"},
        {"vo", "libmpv"}, {"idle", "yes"}, {"keep-open", "yes"},
        {"keep-open-pause", "yes"}, {"video-timing-offset", "0"},
        {"volume", "70"}, {"stop-playback-on-init-failure", "yes"},
    };
    for (size_t i = 0; i < sizeof(options) / sizeof(options[0]); i++) {
        *stage = options[i][0];
        *error = mpv_set_option_string(player->handle, options[i][0], options[i][1]);
        if (*error < 0) goto fail;
    }
    // mpv 0.41 split several built-in Lua services out of load-scripts. They
    // are useful in the standalone player, but an embedded renderer must not
    // start their input/UI workers. Older supported libmpv releases do not
    // know these names, so OPTION_NOT_FOUND is the expected compatibility path.
    const char *builtin_services[] = {
        "load-auto-profiles", "load-commands", "load-console",
        "load-context-menu", "load-positioning", "load-select",
        "load-stats-overlay",
    };
    for (size_t i = 0; i < sizeof(builtin_services) / sizeof(builtin_services[0]); i++) {
        *stage = builtin_services[i];
        *error = mpv_set_option_string(player->handle, builtin_services[i], "no");
        if (*error < 0 && *error != MPV_ERROR_OPTION_NOT_FOUND) goto fail;
    }
    // The source itself is already open through mpv's file stream. These lavf
    // restrictions prohibit nested protocol opens and playlist/concat demuxers.
    char *keys[] = {"protocol_whitelist", "format_whitelist"};
    mpv_node values[] = {
        {.format = MPV_FORMAT_STRING, .u.string = "none"},
        {.format = MPV_FORMAT_STRING, .u.string = "matroska,webm,mov,mp4,m4a,3gp,3g2,mj2,avi,mpeg,mpegts,ogg,asf,flv"},
    };
    mpv_node_list list = {.num = 2, .keys = keys, .values = values};
    mpv_node restrictions = {.format = MPV_FORMAT_NODE_MAP, .u.list = &list};
    *stage = "local container restrictions";
    *error = mpv_set_option(player->handle, "demuxer-lavf-o", MPV_FORMAT_NODE, &restrictions);
    if (*error < 0) goto fail;
    if (audio_output && audio_output[0]) {
        *stage = "audio output";
        *error = mpv_set_option_string(player->handle, "ao", audio_output);
        if (*error < 0) goto fail;
    }
    *stage = "initial pause";
    *error = mpv_set_option(player->handle, "pause", MPV_FORMAT_FLAG, &paused);
    if (*error < 0) goto fail;
    *stage = "initial volume";
    *error = mpv_set_option(player->handle, "volume", MPV_FORMAT_DOUBLE, &volume);
    if (*error < 0) goto fail;
    *stage = "initial mute";
    *error = mpv_set_option(player->handle, "mute", MPV_FORMAT_FLAG, &muted);
    if (*error < 0) goto fail;
    *stage = "initialize player";
    *error = mpv_initialize(player->handle);
    if (*error < 0) goto fail;
    const struct { const char *name; mpv_format format; } properties[] = {
        {"pause", MPV_FORMAT_FLAG}, {"time-pos", MPV_FORMAT_DOUBLE},
        {"duration", MPV_FORMAT_DOUBLE}, {"volume", MPV_FORMAT_DOUBLE},
        {"mute", MPV_FORMAT_FLAG}, {"width", MPV_FORMAT_INT64},
        {"height", MPV_FORMAT_INT64}, {"eof-reached", MPV_FORMAT_FLAG},
    };
    for (size_t i = 0; i < sizeof(properties) / sizeof(properties[0]); i++) {
        *stage = properties[i].name;
        *error = mpv_observe_property(player->handle, i + 1, properties[i].name, properties[i].format);
        if (*error < 0) goto fail;
    }
    mpv_render_param params[] = {
        {MPV_RENDER_PARAM_API_TYPE, MPV_RENDER_API_TYPE_SW},
        {MPV_RENDER_PARAM_INVALID, NULL},
    };
    *stage = "create software renderer";
    *error = mpv_render_context_create(&player->render, player->handle, params);
    if (*error < 0) goto fail;
    mpv_render_context_set_update_callback(player->render, frame_ready, player);
    return player;
fail:
    worldr_media_free(player);
    return NULL;
}

void worldr_media_free(worldr_media *player) {
    if (!player) return;
    if (player->render) mpv_render_context_free(player->render);
    if (player->handle) mpv_terminate_destroy(player->handle);
    free(player);
}

// Only render-thread-safe, asynchronous client calls after renderer creation.
int worldr_media_load(worldr_media *player, const char *path) {
    const char *args[] = {"loadfile", path, "replace", NULL};
    return mpv_command_async(player->handle, 0, args);
}
int worldr_media_pause(worldr_media *player, int pause) {
    return mpv_set_property_async(player->handle, 0, "pause", MPV_FORMAT_FLAG, &pause);
}
int worldr_media_seek(worldr_media *player, const char *seconds) {
    const char *args[] = {"seek", seconds, "absolute+exact", NULL};
    return mpv_command_async(player->handle, 0, args);
}
int worldr_media_volume(worldr_media *player, double volume) {
    return mpv_set_property_async(player->handle, 0, "volume", MPV_FORMAT_DOUBLE, &volume);
}
int worldr_media_mute(worldr_media *player, int mute) {
    return mpv_set_property_async(player->handle, 0, "mute", MPV_FORMAT_FLAG, &mute);
}

worldr_media_event worldr_media_poll(worldr_media *player) {
    mpv_event *event = mpv_wait_event(player->handle, 0);
    worldr_media_event out = {.id = event->event_id, .error = event->error};
    if (event->event_id == MPV_EVENT_PROPERTY_CHANGE) {
        mpv_event_property *property = event->data;
        out.property = event->reply_userdata;
        out.present = property->format != MPV_FORMAT_NONE && property->data;
        if (out.present) {
            switch (property->format) {
            case MPV_FORMAT_FLAG: out.value = *(int *)property->data; break;
            case MPV_FORMAT_DOUBLE: out.value = *(double *)property->data; break;
            case MPV_FORMAT_INT64: out.value = *(int64_t *)property->data; break;
            default: out.present = 0; break;
            }
        }
    } else if (event->event_id == MPV_EVENT_END_FILE) {
        mpv_event_end_file *end = event->data;
        out.end_reason = end->reason;
        out.error = end->error;
    }
    return out;
}

int worldr_media_render(worldr_media *player, uint8_t *pixels, int width, int height, size_t stride, int force) {
    int ready = atomic_exchange_explicit(&player->redraw, 0, memory_order_relaxed);
    if (!ready && !force) return 0;
    uint64_t update = mpv_render_context_update(player->render);
    if (!(update & MPV_RENDER_UPDATE_FRAME) && !force) return 0;
    int size[] = {width, height};
    int block = 0;
    mpv_render_param params[] = {
        {MPV_RENDER_PARAM_SW_SIZE, size},
        {MPV_RENDER_PARAM_SW_FORMAT, "rgb0"},
        {MPV_RENDER_PARAM_SW_STRIDE, &stride},
        {MPV_RENDER_PARAM_SW_POINTER, pixels},
        {MPV_RENDER_PARAM_BLOCK_FOR_TARGET_TIME, &block},
        {MPV_RENDER_PARAM_INVALID, NULL},
    };
    int error = mpv_render_context_render(player->render, params);
    if (error < 0) return error;
    // mpv's portable 32-bit software formats leave the alpha byte undefined.
    // Worldr samples this surface as RGBA, so every video pixel must be opaque.
    for (int y = 0; y < height; y++) {
        uint8_t *row = pixels + (size_t)y * stride;
        for (int x = 0; x < width; x++) row[(size_t)x * 4 + 3] = 255;
    }
    return 1;
}
