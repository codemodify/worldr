/* Independent GBM producer used only by opt-in DMA-BUF integration tests. */
#define _GNU_SOURCE
#include <gbm.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <fcntl.h>
#include <string.h>
#include <sys/socket.h>

#ifndef GBM_FORMAT_XRGB2101010
#define GBM_FORMAT_XRGB2101010 0x30335258u
#endif
#ifndef GBM_FORMAT_ARGB2101010
#define GBM_FORMAT_ARGB2101010 0x30335241u
#endif
#ifndef GBM_FORMAT_XBGR2101010
#define GBM_FORMAT_XBGR2101010 0x30334258u
#endif
#ifndef GBM_FORMAT_ABGR2101010
#define GBM_FORMAT_ABGR2101010 0x30334241u
#endif

static int is_xrgb(uint32_t format)
{
 return format==GBM_FORMAT_XRGB8888||format==GBM_FORMAT_XBGR8888||
        format==GBM_FORMAT_XRGB2101010||format==GBM_FORMAT_XBGR2101010;
}
static int is_bgr(uint32_t format)
{
 return format==GBM_FORMAT_XBGR8888||format==GBM_FORMAT_ABGR8888||
        format==GBM_FORMAT_XBGR2101010||format==GBM_FORMAT_ABGR2101010;
}
static int is_2101010(uint32_t format)
{
 return format==GBM_FORMAT_XRGB2101010||format==GBM_FORMAT_ARGB2101010||
        format==GBM_FORMAT_XBGR2101010||format==GBM_FORMAT_ABGR2101010;
}
static uint32_t pixel(uint32_t format,uint8_t r,uint8_t g,uint8_t b,uint8_t a)
{
 if(is_2101010(format)){
  uint32_t r10=(uint32_t)r*1023/255,g10=(uint32_t)g*1023/255,b10=(uint32_t)b*1023/255;
  uint32_t a2=is_xrgb(format)?0:((uint32_t)a*3+127)/255;
  return is_bgr(format)?(a2<<30)|(b10<<20)|(g10<<10)|r10:
                        (a2<<30)|(r10<<20)|(g10<<10)|b10;
 }
 uint32_t a8=is_xrgb(format)?0:a;
 return is_bgr(format)?(a8<<24)|((uint32_t)b<<16)|((uint32_t)g<<8)|r:
                       (a8<<24)|((uint32_t)r<<16)|((uint32_t)g<<8)|b;
}
static int supported_format(uint32_t format)
{
 return format==GBM_FORMAT_XRGB8888||format==GBM_FORMAT_ARGB8888||
        format==GBM_FORMAT_XBGR8888||format==GBM_FORMAT_ABGR8888||
        format==GBM_FORMAT_XRGB2101010||format==GBM_FORMAT_ARGB2101010||
        format==GBM_FORMAT_XBGR2101010||format==GBM_FORMAT_ABGR2101010;
}
static int fill(struct gbm_bo *bo,uint32_t format,int replacement)
{
 uint32_t stride;void *mapping=NULL;uint8_t *p=gbm_bo_map(bo,0,0,32,32,GBM_BO_TRANSFER_WRITE,&stride,&mapping);
 if(!p)return -1;
 for(unsigned y=0;y<32;y++)for(unsigned x=0;x<32;x++){
  uint32_t *q=(uint32_t *)(p+y*stride+x*4);
  if(replacement)*q=pixel(format,0,0,255,255);
  else if(y<16&&x<16)*q=pixel(format,255,0,0,255);
  else if(y<16)*q=pixel(format,0,255,0,255);
  else if(x<16)*q=pixel(format,0,0,128,128);
  else *q=pixel(format,255,255,0,255);
 }
 gbm_bo_unmap(bo,mapping);return 0;
}
int main(int argc,char **argv)
{
 int drm=open(argc>1?argv[1]:"/dev/dri/renderD128",O_RDWR|O_CLOEXEC);if(drm<0){perror("open render device");return 2;}
 struct gbm_device *dev=gbm_create_device(drm);if(!dev)return 3;
 const char *requested=getenv("WORLDR_DMABUF_FIXTURE_FORMAT");
 uint32_t format=requested?(uint32_t)strtoul(requested,NULL,0):GBM_FORMAT_ARGB8888;
 if(!supported_format(format)){fprintf(stderr,"unsupported fixture format %#x\n",format);return 4;}
 const char *requested_modifier=getenv("WORLDR_DMABUF_FIXTURE_MODIFIER");
 uint64_t modifier=requested_modifier?strtoull(requested_modifier,NULL,0):0;
 uint32_t usage=GBM_BO_USE_RENDERING|(modifier?0:GBM_BO_USE_LINEAR);
 struct gbm_bo *bo=gbm_bo_create_with_modifiers2(dev,32,32,format,&modifier,1,usage);
 if(!bo&&!modifier)bo=gbm_bo_create(dev,32,32,format,usage);
 if(!bo||gbm_bo_get_modifier(bo)!=modifier){fprintf(stderr,"GBM explicit-modifier allocation unavailable: backend=%s bo=%p requested=%llu actual=%llu\n",gbm_device_get_backend_name(dev),(void*)bo,(unsigned long long)modifier,bo?(unsigned long long)gbm_bo_get_modifier(bo):0);perror("GBM");return 4;}
 if(fill(bo,format,0))return 5;
 int fd=gbm_bo_get_fd(bo);if(fd<0)return 6;
 uint32_t payload[5]={32,32,gbm_bo_get_stride(bo),gbm_bo_get_offset(bo,0),format};
 char control[CMSG_SPACE(sizeof(int))]={0};struct iovec io={payload,sizeof(payload)};
 struct msghdr msg={.msg_iov=&io,.msg_iovlen=1,.msg_control=control,.msg_controllen=sizeof(control)};
 struct cmsghdr *c=CMSG_FIRSTHDR(&msg);c->cmsg_level=SOL_SOCKET;c->cmsg_type=SCM_RIGHTS;c->cmsg_len=CMSG_LEN(sizeof(int));memcpy(CMSG_DATA(c),&fd,sizeof(fd));
 if(sendmsg(3,&msg,0)!=(int)sizeof(payload))return 7;
 close(fd);
 char command;
 while(read(3,&command,1)==1){if(command=='m'){if(fill(bo,format,1))return 8;write(3,"m",1);}else if(command=='q')break;}
 gbm_bo_destroy(bo);gbm_device_destroy(dev);close(drm);return 0;
}
