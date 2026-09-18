//go:build linux && cgo

#include "clipboard.h"
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define CLIPBOARD_MIMES 64
#define CLIPBOARD_OFFERS 16
#define CLIPBOARD_REQUESTS 32
#define CLIPBOARD_MIME_LENGTH 1024

struct clipboard_offer {
    struct worldr_host_clipboard *clipboard;
    struct wl_data_offer *proxy;
    struct clipboard_offer *next;
    uint64_t id;
    char *mimes[CLIPBOARD_MIMES];
    int count;
};
struct clipboard_source {
    struct worldr_host_clipboard *clipboard;
    struct wl_data_source *proxy;
    uint64_t id, external_id;
    char *mimes[CLIPBOARD_MIMES];
    int count;
};
struct worldr_host_clipboard {
    struct wl_data_device_manager *manager;
    struct wl_data_device *device;
    struct wl_seat *seat;
    struct clipboard_offer *offers, *selected;
    struct clipboard_source *source;
    uint32_t manager_name, serial;
    uint64_t revision, next_id;
    int focused, offer_count, request_count;
    worldr_clipboard_request requests[CLIPBOARD_REQUESTS];
};

static void discard_requests(worldr_host_clipboard *c) {
    for(int i=0;i<c->request_count;i++) {
        close(c->requests[i].fd);
        free(c->requests[i].mime);
    }
    c->request_count=0;
}
static int has_mime(char *const *mimes, int count, const char *mime) {
    for(int i=0;i<count;i++)if(!strcmp(mimes[i],mime))return 1;
    return 0;
}
static void destroy_offer(worldr_host_clipboard *c, struct clipboard_offer *offer) {
    struct clipboard_offer **p=&c->offers;
    while(*p && *p!=offer)p=&(*p)->next;
    if(!*p)return;
    *p=offer->next;
    if(c->selected==offer)c->selected=NULL;
    for(int i=0;i<offer->count;i++)free(offer->mimes[i]);
    wl_data_offer_destroy(offer->proxy);
    free(offer);
    c->offer_count--;
}
static void clear_offers(worldr_host_clipboard *c) {
    while(c->offers)destroy_offer(c,c->offers);
}
static void destroy_source(worldr_host_clipboard *c) {
    if(!c->source)return;
    struct clipboard_source *source=c->source;
    c->source=NULL;
    for(int i=0;i<source->count;i++)free(source->mimes[i]);
    wl_data_source_destroy(source->proxy);
    free(source);
    discard_requests(c);
}
static void offer_mime(void *data, struct wl_data_offer *proxy, const char *mime) {
    (void)proxy;
    struct clipboard_offer *offer=data;
    if(!mime || !*mime || strlen(mime)>CLIPBOARD_MIME_LENGTH || offer->count==CLIPBOARD_MIMES || has_mime(offer->mimes,offer->count,mime))return;
    char *copy=strdup(mime);
    if(copy)offer->mimes[offer->count++]=copy;
}
static void offer_source_actions(void *data,struct wl_data_offer *proxy,uint32_t actions) {(void)data;(void)proxy;(void)actions;}
static void offer_action(void *data,struct wl_data_offer *proxy,uint32_t action) {(void)data;(void)proxy;(void)action;}
static const struct wl_data_offer_listener offer_listener={.offer=offer_mime,.source_actions=offer_source_actions,.action=offer_action};
static void device_offer(void *data,struct wl_data_device *device,struct wl_data_offer *proxy) {
    (void)device;
    worldr_host_clipboard *c=data;
    if(c->offer_count==CLIPBOARD_OFFERS){wl_data_offer_destroy(proxy);return;}
    struct clipboard_offer *offer=calloc(1,sizeof(*offer));
    if(!offer){wl_data_offer_destroy(proxy);return;}
    offer->clipboard=c;offer->proxy=proxy;offer->id=++c->next_id;
    offer->next=c->offers;c->offers=offer;c->offer_count++;
    wl_data_offer_add_listener(proxy,&offer_listener,offer);
}
static struct clipboard_offer *find_offer(worldr_host_clipboard *c,struct wl_data_offer *proxy) {
    for(struct clipboard_offer *offer=c->offers;offer;offer=offer->next)if(offer->proxy==proxy)return offer;
    return NULL;
}
static void device_selection(void *data,struct wl_data_device *device,struct wl_data_offer *proxy) {
    (void)device;
    worldr_host_clipboard *c=data;
    struct clipboard_offer *next=proxy?find_offer(c,proxy):NULL;
    if(c->selected && c->selected!=next)destroy_offer(c,c->selected);
    c->selected=next;
    c->revision++;
    if(!c->focused && c->selected)destroy_offer(c,c->selected);
}
static void device_enter(void *data,struct wl_data_device *device,uint32_t serial,struct wl_surface *surface,wl_fixed_t x,wl_fixed_t y,struct wl_data_offer *proxy) {
    (void)device;(void)serial;(void)surface;(void)x;(void)y;
    // Drag-and-drop is a separate policy; reject its offer without reading it.
    worldr_host_clipboard *c=data;
    struct clipboard_offer *offer=find_offer(c,proxy);
    if(offer && offer!=c->selected)destroy_offer(c,offer);
}
static void device_leave(void *data,struct wl_data_device *device){(void)data;(void)device;}
static void device_motion(void *data,struct wl_data_device *device,uint32_t time,wl_fixed_t x,wl_fixed_t y){(void)data;(void)device;(void)time;(void)x;(void)y;}
static void device_drop(void *data,struct wl_data_device *device){(void)data;(void)device;}
static const struct wl_data_device_listener device_listener={.data_offer=device_offer,.enter=device_enter,.leave=device_leave,.motion=device_motion,.drop=device_drop,.selection=device_selection};
static void source_target(void *data,struct wl_data_source *source,const char *mime){(void)data;(void)source;(void)mime;}
static void source_send(void *data,struct wl_data_source *proxy,const char *mime,int32_t fd) {
    (void)proxy;
    struct clipboard_source *source=data;
    worldr_host_clipboard *c=source->clipboard;
    if(c->source!=source || c->request_count==CLIPBOARD_REQUESTS || !mime || !has_mime(source->mimes,source->count,mime)){close(fd);return;}
    char *copy=strdup(mime);
    if(!copy){close(fd);return;}
    c->requests[c->request_count++]=(worldr_clipboard_request){.external_id=source->external_id,.mime=copy,.fd=fd};
}
static void source_cancelled(void *data,struct wl_data_source *proxy) {
    (void)proxy;
    struct clipboard_source *source=data;
    worldr_host_clipboard *c=source->clipboard;
    if(c->source==source){destroy_source(c);clear_offers(c);c->revision++;}
}
static void source_drop_performed(void *data,struct wl_data_source *source){(void)data;(void)source;}
static void source_finished(void *data,struct wl_data_source *source){(void)data;(void)source;}
static void source_action(void *data,struct wl_data_source *source,uint32_t action){(void)data;(void)source;(void)action;}
static const struct wl_data_source_listener source_listener={.target=source_target,.send=source_send,.cancelled=source_cancelled,.dnd_drop_performed=source_drop_performed,.dnd_finished=source_finished,.action=source_action};
static void connect_device(worldr_host_clipboard *c) {
    if(c->manager && c->seat && !c->device){
        c->device=wl_data_device_manager_get_data_device(c->manager,c->seat);
        if(c->device)wl_data_device_add_listener(c->device,&device_listener,c);
    }
}
static void disconnect_device(worldr_host_clipboard *c) {
    clear_offers(c);destroy_source(c);discard_requests(c);
    if(c->device){
        if(wl_data_device_get_version(c->device)>=2)wl_data_device_release(c->device);
        else wl_data_device_destroy(c->device);
        c->device=NULL;
    }
    c->focused=0;c->serial=0;c->revision++;
}
worldr_host_clipboard *worldr_host_clipboard_new(void){return calloc(1,sizeof(worldr_host_clipboard));}
void worldr_host_clipboard_close(worldr_host_clipboard *c) {
    if(!c)return;
    disconnect_device(c);
    if(c->manager)wl_data_device_manager_destroy(c->manager);
    free(c);
}
void worldr_host_clipboard_global(worldr_host_clipboard *c,struct wl_registry *registry,uint32_t name,uint32_t version) {
    if(!c || c->manager)return;
    c->manager=wl_registry_bind(registry,name,&wl_data_device_manager_interface,version<3?version:3);
    c->manager_name=name;connect_device(c);
}
void worldr_host_clipboard_removed(worldr_host_clipboard *c,uint32_t name) {
    if(!c || !c->manager || c->manager_name!=name)return;
    disconnect_device(c);wl_data_device_manager_destroy(c->manager);c->manager=NULL;c->manager_name=0;
}
void worldr_host_clipboard_seat(worldr_host_clipboard *c,struct wl_seat *seat) {
    if(!c || c->seat==seat)return;
    if(c->seat)disconnect_device(c);
    c->seat=seat;connect_device(c);
}
void worldr_host_clipboard_focus(worldr_host_clipboard *c,int focused,uint32_t serial) {
    if(!c)return;
    c->focused=focused;
    if(focused)c->serial=serial;
    else {c->serial=0;clear_offers(c);c->revision++;}
    // Keep an outgoing source alive: users paste after switching to another app.
}
void worldr_host_clipboard_serial(worldr_host_clipboard *c,uint32_t serial){if(c && c->focused)c->serial=serial;}
worldr_clipboard_offer worldr_host_clipboard_offer(worldr_host_clipboard *c) {
    worldr_clipboard_offer result={0};
    if(!c)return result;
    result.revision=c->revision;
    result.available=c->device && c->focused;
    if(c->source){result.id=c->source->id;result.external_id=c->source->external_id;result.mime_count=c->source->count;}
    else if(c->selected && c->focused){result.id=c->selected->id;result.mime_count=c->selected->count;}
    return result;
}
const char *worldr_host_clipboard_mime(worldr_host_clipboard *c,int index) {
    if(!c || index<0)return NULL;
    if(c->source)return index<c->source->count?c->source->mimes[index]:NULL;
    if(c->selected && c->focused)return index<c->selected->count?c->selected->mimes[index]:NULL;
    return NULL;
}
int worldr_host_clipboard_receive(worldr_host_clipboard *c,uint64_t id,const char *mime,int fd) {
    if(!c || !c->device || !c->focused)return -1;
    if(c->source || !c->selected || c->selected->id!=id || fd<0 || !mime || !has_mime(c->selected->mimes,c->selected->count,mime))return -2;
    wl_data_offer_receive(c->selected->proxy,mime,fd);
    return 0;
}
int worldr_host_clipboard_publish(worldr_host_clipboard *c,uint64_t external_id,const char *const *mimes,int count) {
    if(!c)return -1;
    if(external_id==0){
        if(c->source){
            if(c->device && c->focused && c->serial)wl_data_device_set_selection(c->device,NULL,c->serial);
            destroy_source(c);clear_offers(c);c->revision++;
        }
        return 0;
    }
    if(!c->manager || !c->device || !c->focused || !c->serial)return -1;
    if(count<1 || count>CLIPBOARD_MIMES)return -2;
    struct clipboard_source *source=calloc(1,sizeof(*source));
    if(!source)return -3;
    source->clipboard=c;source->external_id=external_id;source->id=++c->next_id;
    for(int i=0;i<count;i++){
        if(!mimes[i] || !*mimes[i] || strlen(mimes[i])>CLIPBOARD_MIME_LENGTH)goto fail;
        source->mimes[i]=strdup(mimes[i]);
        if(!source->mimes[i])goto fail;
        source->count++;
    }
    source->proxy=wl_data_device_manager_create_data_source(c->manager);
    if(!source->proxy)goto fail;
    wl_data_source_add_listener(source->proxy,&source_listener,source);
    for(int i=0;i<count;i++)wl_data_source_offer(source->proxy,source->mimes[i]);
    wl_data_device_set_selection(c->device,source->proxy,c->serial);
    destroy_source(c);clear_offers(c);c->source=source;c->revision++;
    return 0;
fail:
    for(int i=0;i<source->count;i++)free(source->mimes[i]);
    free(source);return -3;
}
int worldr_host_clipboard_requests(worldr_host_clipboard *c,worldr_clipboard_request *out,int capacity) {
    if(!c || capacity<=0)return 0;
    int count=c->request_count<capacity?c->request_count:capacity;
    memcpy(out,c->requests,(size_t)count*sizeof(*out));
    memmove(c->requests,c->requests+count,(size_t)(c->request_count-count)*sizeof(*out));
    c->request_count-=count;
    return count;
}
