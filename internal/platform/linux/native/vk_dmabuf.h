/* Explicit-modifier, single-plane 32-bit RGB(A/X) DMA-BUF import. Client images
 * are copied on the GPU into LINEAR snapshots; the copy fence completes before
 * the caller may release wl_buffer. Exported snapshots outlive this device. */
#define WORLDR_DRM_ARGB 0x34325241u
#define WORLDR_DRM_XRGB 0x34325258u
#define WORLDR_DRM_ABGR 0x34324241u
#define WORLDR_DRM_XBGR 0x34324258u
#define WORLDR_DRM_ARGB2101010 0x30335241u
#define WORLDR_DRM_XRGB2101010 0x30335258u
#define WORLDR_DRM_ABGR2101010 0x30334241u
#define WORLDR_DRM_XBGR2101010 0x30334258u
static VkFormat dmabuf_vk_format(uint32_t format)
{
 if(format==WORLDR_DRM_ARGB||format==WORLDR_DRM_XRGB)return VK_FORMAT_B8G8R8A8_UNORM;
 if(format==WORLDR_DRM_ABGR||format==WORLDR_DRM_XBGR)return VK_FORMAT_R8G8B8A8_UNORM;
 if(format==WORLDR_DRM_ARGB2101010||format==WORLDR_DRM_XRGB2101010)return VK_FORMAT_A2R10G10B10_UNORM_PACK32;
 if(format==WORLDR_DRM_ABGR2101010||format==WORLDR_DRM_XBGR2101010)return VK_FORMAT_A2B10G10R10_UNORM_PACK32;
 return VK_FORMAT_UNDEFINED;
}
static int dmabuf_query(worldr_vk *vk,VkFormat format,uint64_t modifier,VkImageUsageFlags usage,VkExternalMemoryFeatureFlags required)
{
 if(!vk||!vk->device||!vk->dmabuf)return 0;
 VkPhysicalDeviceExternalImageFormatInfo external={.sType=VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_EXTERNAL_IMAGE_FORMAT_INFO,.handleType=VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT};
 VkPhysicalDeviceImageDrmFormatModifierInfoEXT modifier_info={.sType=VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_IMAGE_DRM_FORMAT_MODIFIER_INFO_EXT,.pNext=&external,.drmFormatModifier=modifier,.sharingMode=VK_SHARING_MODE_EXCLUSIVE};
 VkPhysicalDeviceImageFormatInfo2 info={.sType=VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_IMAGE_FORMAT_INFO_2,.pNext=&modifier_info,.format=format,.type=VK_IMAGE_TYPE_2D,.tiling=VK_IMAGE_TILING_DRM_FORMAT_MODIFIER_EXT,.usage=usage};
 VkExternalImageFormatProperties ext={.sType=VK_STRUCTURE_TYPE_EXTERNAL_IMAGE_FORMAT_PROPERTIES};
 VkImageFormatProperties2 properties={.sType=VK_STRUCTURE_TYPE_IMAGE_FORMAT_PROPERTIES_2,.pNext=&ext};
 return vkGetPhysicalDeviceImageFormatProperties2(vk->phys,&info,&properties)==VK_SUCCESS &&
  (ext.externalMemoryProperties.externalMemoryFeatures&required)==required;
}
int worldr_vk_dmabuf_format_supported(worldr_vk *vk,uint32_t format)
{
 VkFormat vk_format=dmabuf_vk_format(format);
 if(vk_format==VK_FORMAT_UNDEFINED)return 0;
 VkFormatProperties features;
 vkGetPhysicalDeviceFormatProperties(vk->phys,vk_format,&features);
 VkImageFormatProperties sampled;
 return (features.optimalTilingFeatures&VK_FORMAT_FEATURE_SAMPLED_IMAGE_FILTER_LINEAR_BIT)&&
  vkGetPhysicalDeviceImageFormatProperties(vk->phys,vk_format,VK_IMAGE_TYPE_2D,VK_IMAGE_TILING_OPTIMAL,
   VK_IMAGE_USAGE_SAMPLED_BIT|VK_IMAGE_USAGE_TRANSFER_DST_BIT,0,&sampled)==VK_SUCCESS&&
  dmabuf_query(vk,vk_format,0,VK_IMAGE_USAGE_TRANSFER_SRC_BIT,VK_EXTERNAL_MEMORY_FEATURE_IMPORTABLE_BIT)&&
  dmabuf_query(vk,vk_format,0,VK_IMAGE_USAGE_TRANSFER_SRC_BIT|VK_IMAGE_USAGE_TRANSFER_DST_BIT,VK_EXTERNAL_MEMORY_FEATURE_EXPORTABLE_BIT|VK_EXTERNAL_MEMORY_FEATURE_IMPORTABLE_BIT);
}
int worldr_vk_dmabuf_modifier_supported(worldr_vk *vk,uint32_t format,uint64_t modifier)
{
 VkFormat vk_format=dmabuf_vk_format(format);
 return modifier!=UINT64_MAX&&vk_format!=VK_FORMAT_UNDEFINED&&
  worldr_vk_dmabuf_format_supported(vk,format)&&
  dmabuf_query(vk,vk_format,modifier,VK_IMAGE_USAGE_TRANSFER_SRC_BIT,VK_EXTERNAL_MEMORY_FEATURE_IMPORTABLE_BIT);
}
int worldr_vk_dmabuf_modifiers(worldr_vk *vk,uint32_t format,uint64_t *out,int capacity)
{
 if(capacity<0||!worldr_vk_dmabuf_format_supported(vk,format))return 0;
 VkFormat vk_format=dmabuf_vk_format(format);
 VkDrmFormatModifierPropertiesListEXT list={.sType=VK_STRUCTURE_TYPE_DRM_FORMAT_MODIFIER_PROPERTIES_LIST_EXT};
 VkFormatProperties2 properties={.sType=VK_STRUCTURE_TYPE_FORMAT_PROPERTIES_2,.pNext=&list};
 vkGetPhysicalDeviceFormatProperties2(vk->phys,vk_format,&properties);
 uint32_t available=list.drmFormatModifierCount;
 VkDrmFormatModifierPropertiesEXT *items=available?calloc(available,sizeof(*items)):NULL;
 if(available&&!items)return 0;
 list.pDrmFormatModifierProperties=items;
 if(available)vkGetPhysicalDeviceFormatProperties2(vk->phys,vk_format,&properties);
 int count=0;
 int linear=0;
 for(uint32_t i=0;i<list.drmFormatModifierCount;i++)
  if(!items[i].drmFormatModifier&&items[i].drmFormatModifierPlaneCount==1&&
     (items[i].drmFormatModifierTilingFeatures&VK_FORMAT_FEATURE_TRANSFER_SRC_BIT)){
   linear=1;break;
  }
 if(linear&&worldr_vk_dmabuf_modifier_supported(vk,format,0)){
  if(out&&count<capacity)out[count]=0;
  count++;
 }
 for(uint32_t i=0;i<list.drmFormatModifierCount;i++){
  uint64_t modifier=items[i].drmFormatModifier;
  if(!modifier||modifier==UINT64_MAX||items[i].drmFormatModifierPlaneCount!=1||
     !(items[i].drmFormatModifierTilingFeatures&VK_FORMAT_FEATURE_TRANSFER_SRC_BIT)||
     !worldr_vk_dmabuf_modifier_supported(vk,format,modifier))continue;
  int duplicate=0;
  for(uint32_t j=0;j<i;j++)if(items[j].drmFormatModifier==modifier){duplicate=1;break;}
  if(duplicate)continue;
  if(out&&count<capacity)out[count]=modifier;
  count++;
 }
 free(items);return count;
}
int worldr_vk_dmabuf_supported(worldr_vk *vk)
{
	if(!vk||!vk->device||!vk->dmabuf)return 0;
	if(vk->dmabuf_formats>=0)return vk->dmabuf_formats;
 return vk->dmabuf_formats=worldr_vk_dmabuf_format_supported(vk,WORLDR_DRM_ARGB)||
  worldr_vk_dmabuf_format_supported(vk,WORLDR_DRM_ABGR)||
  worldr_vk_dmabuf_format_supported(vk,WORLDR_DRM_ARGB2101010)||
  worldr_vk_dmabuf_format_supported(vk,WORLDR_DRM_ABGR2101010);
}
static uint64_t dmabuf_milliseconds(void)
{
 struct timespec now;clock_gettime(CLOCK_MONOTONIC,&now);return (uint64_t)now.tv_sec*1000+now.tv_nsec/1000000;
}
static int dmabuf_wait(int fd,char *err,int errlen)
{
 uint64_t end=dmabuf_milliseconds()+2000;
 for(;;){
  uint64_t now=dmabuf_milliseconds();if(now>=end){seterr(err,errlen,"DMA-BUF producer wait failed (2s limit)",VK_TIMEOUT);return -1;}
  struct pollfd p={.fd=fd,.events=POLLIN};int n=poll(&p,1,(int)(end-now));
  if(n<0&&errno==EINTR)continue;
  if(n>0&&(p.revents&POLLIN)&&!(p.revents&(POLLERR|POLLNVAL)))return 0;
  if(n==0){seterr(err,errlen,"DMA-BUF producer wait failed (2s limit)",VK_TIMEOUT);return -1;}
  seterr(err,errlen,"DMA-BUF producer fence unavailable",VK_ERROR_INVALID_EXTERNAL_HANDLE);return -1;
 }
}
static int dmabuf_image(worldr_vk *vk,const worldr_dmabuf *d,int importing,scene_image *out,char *err,int errlen)
{
 VkResult r;int owned_fd=-1;VkDeviceSize fd_size=0;
 VkFormat vk_format=d?dmabuf_vk_format(d->format):VK_FORMAT_UNDEFINED;
 int supported=d&&((importing&&worldr_vk_dmabuf_modifier_supported(vk,d->format,d->modifier))||
                   (!importing&&!d->modifier&&worldr_vk_dmabuf_format_supported(vk,d->format)));
 if(!d||!d->width||!d->height||d->width>4096||d->height>4096||vk_format==VK_FORMAT_UNDEFINED||!supported){
  seterr(err,errlen,"unsupported DMA-BUF descriptor",VK_ERROR_FORMAT_NOT_SUPPORTED);return -1;
 }
 PFN_vkGetMemoryFdPropertiesKHR fd_properties=(PFN_vkGetMemoryFdPropertiesKHR)vkGetDeviceProcAddr(vk->device,"vkGetMemoryFdPropertiesKHR");
 VkMemoryFdPropertiesKHR fdprops={.sType=VK_STRUCTURE_TYPE_MEMORY_FD_PROPERTIES_KHR};
 if(importing){
  if(d->fd<0||d->stride<d->width*4||d->stride>1048576||d->stride%4||d->offset%4){seterr(err,errlen,"invalid DMA-BUF plane layout",VK_ERROR_INVALID_EXTERNAL_HANDLE);return -1;}
  if(!fd_properties||(r=fd_properties(vk->device,VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT,d->fd,&fdprops))!=VK_SUCCESS){seterr(err,errlen,"DMA-BUF memory handle rejected",VK_ERROR_INVALID_EXTERNAL_HANDLE);return -1;}
  off_t length=lseek(d->fd,0,SEEK_END);
  if(length<=0||(uint64_t)d->offset+(uint64_t)d->stride*(d->height-1)+(uint64_t)d->width*4>(uint64_t)length){seterr(err,errlen,"DMA-BUF plane exceeds allocation",VK_ERROR_INVALID_EXTERNAL_HANDLE);return -1;}
  fd_size=(VkDeviceSize)length;
  if(dmabuf_wait(d->fd,err,errlen))return -1;
 }
 VkExternalMemoryImageCreateInfo external={.sType=VK_STRUCTURE_TYPE_EXTERNAL_MEMORY_IMAGE_CREATE_INFO,.handleTypes=VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT};
 VkSubresourceLayout plane={.offset=d->offset,.rowPitch=d->stride};
 VkImageDrmFormatModifierExplicitCreateInfoEXT explicit={.sType=VK_STRUCTURE_TYPE_IMAGE_DRM_FORMAT_MODIFIER_EXPLICIT_CREATE_INFO_EXT,.pNext=&external,.drmFormatModifier=d->modifier,.drmFormatModifierPlaneCount=1,.pPlaneLayouts=&plane};
 VkImageDrmFormatModifierListCreateInfoEXT list={.sType=VK_STRUCTURE_TYPE_IMAGE_DRM_FORMAT_MODIFIER_LIST_CREATE_INFO_EXT,.pNext=&external,.drmFormatModifierCount=1,.pDrmFormatModifiers=&d->modifier};
 VkImageCreateInfo info={.sType=VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,.pNext=importing?(void *)&explicit:(void *)&list,
  .imageType=VK_IMAGE_TYPE_2D,.format=vk_format,.extent={d->width,d->height,1},.mipLevels=1,.arrayLayers=1,.samples=VK_SAMPLE_COUNT_1_BIT,
  .tiling=VK_IMAGE_TILING_DRM_FORMAT_MODIFIER_EXT,.usage=VK_IMAGE_USAGE_TRANSFER_SRC_BIT|(importing?0:VK_IMAGE_USAGE_TRANSFER_DST_BIT),.sharingMode=VK_SHARING_MODE_EXCLUSIVE,.initialLayout=VK_IMAGE_LAYOUT_UNDEFINED};
 if((r=vkCreateImage(vk->device,&info,NULL,&out->image))!=VK_SUCCESS)goto fail;
 VkMemoryRequirements req;vkGetImageMemoryRequirements(vk->device,out->image,&req);
 if(importing&&req.size>fd_size){seterr(err,errlen,"DMA-BUF allocation is smaller than Vulkan image requirements",VK_ERROR_INVALID_EXTERNAL_HANDLE);goto resource_fail;}
 uint32_t type=find_memory(vk->phys,req.memoryTypeBits&(importing?fdprops.memoryTypeBits:UINT32_MAX),0);
 if(type==UINT32_MAX){r=VK_ERROR_INVALID_EXTERNAL_HANDLE;goto fail;}
 if(!memory_allow(vk,req.size,err,errlen))goto resource_fail;
 VkMemoryDedicatedAllocateInfo dedicated={.sType=VK_STRUCTURE_TYPE_MEMORY_DEDICATED_ALLOCATE_INFO,.image=out->image};
 VkImportMemoryFdInfoKHR import={.sType=VK_STRUCTURE_TYPE_IMPORT_MEMORY_FD_INFO_KHR,.pNext=&dedicated,.handleType=VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT};
 VkExportMemoryAllocateInfo export={.sType=VK_STRUCTURE_TYPE_EXPORT_MEMORY_ALLOCATE_INFO,.pNext=&dedicated,.handleTypes=VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT};
 if(importing){owned_fd=fcntl(d->fd,F_DUPFD_CLOEXEC,0);if(owned_fd<0){r=VK_ERROR_TOO_MANY_OBJECTS;goto fail;}import.fd=owned_fd;}
 VkMemoryAllocateInfo allocation={.sType=VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,.pNext=importing?(void *)&import:(void *)&export,.allocationSize=req.size,.memoryTypeIndex=type};
 if((r=vkAllocateMemory(vk->device,&allocation,NULL,&out->memory))!=VK_SUCCESS)goto fail;
 owned_fd=-1;out->allocation=req.size;memory_added(vk,req.size,1);
 if((r=vkBindImageMemory(vk->device,out->image,out->memory,0))!=VK_SUCCESS)goto fail;
 return 0;
 fail:seterr(err,errlen,"DMA-BUF image allocation/import failed",r);
 resource_fail:if(owned_fd>=0)close(owned_fd);scene_drop_image(vk,out);return -1;
}
static void dmabuf_acquire(worldr_vk *vk,scene_image *image)
{
 VkImageMemoryBarrier b={.sType=VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER,.oldLayout=VK_IMAGE_LAYOUT_GENERAL,.newLayout=VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
  .srcQueueFamilyIndex=VK_QUEUE_FAMILY_FOREIGN_EXT,.dstQueueFamilyIndex=vk->queue_family,.dstAccessMask=VK_ACCESS_TRANSFER_READ_BIT,
  .image=image->image,.subresourceRange={.aspectMask=VK_IMAGE_ASPECT_COLOR_BIT,.levelCount=1,.layerCount=1}};
 vkCmdPipelineBarrier(vk->scene->cmd,VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT,VK_PIPELINE_STAGE_TRANSFER_BIT,0,0,NULL,0,NULL,1,&b);
}
static void dmabuf_release(worldr_vk *vk,scene_image *image,int written)
{
 VkImageMemoryBarrier b={.sType=VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER,.oldLayout=written?VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL:VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,.newLayout=VK_IMAGE_LAYOUT_GENERAL,
  .srcQueueFamilyIndex=vk->queue_family,.dstQueueFamilyIndex=VK_QUEUE_FAMILY_FOREIGN_EXT,.srcAccessMask=written?VK_ACCESS_TRANSFER_WRITE_BIT:VK_ACCESS_TRANSFER_READ_BIT,
  .image=image->image,.subresourceRange={.aspectMask=VK_IMAGE_ASPECT_COLOR_BIT,.levelCount=1,.layerCount=1}};
 vkCmdPipelineBarrier(vk->scene->cmd,VK_PIPELINE_STAGE_TRANSFER_BIT,VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT,0,0,NULL,0,NULL,1,&b);
}
int worldr_vk_copy_dmabuf(worldr_vk *vk,const worldr_dmabuf *source,worldr_dmabuf *snapshot,char *err,int errlen)
{
 if(!worldr_vk_dmabuf_supported(vk)){seterr(err,errlen,"DMA-BUF sharing unsupported",VK_ERROR_FEATURE_NOT_PRESENT);return -1;}
 if(scene_init(vk,err,errlen)||scene_wait(vk,err,errlen))return -1;
 worldr_scene *s=vk->scene;scene_drop_image(vk,&s->pending_import);scene_drop_image(vk,&s->pending_export);
 worldr_dmabuf linear=*source;linear.modifier=0;linear.offset=0;linear.stride=0;
 if(dmabuf_image(vk,source,1,&s->pending_import,err,errlen)||dmabuf_image(vk,&linear,0,&s->pending_export,err,errlen))goto fail;
 if(scene_begin(vk,err,errlen))goto fail;
 dmabuf_acquire(vk,&s->pending_import);
 barrier(s->cmd,s->pending_export.image,VK_IMAGE_LAYOUT_UNDEFINED,VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,0,VK_ACCESS_TRANSFER_WRITE_BIT,VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT,VK_PIPELINE_STAGE_TRANSFER_BIT);
 VkImageCopy copy={.srcSubresource={.aspectMask=VK_IMAGE_ASPECT_COLOR_BIT,.layerCount=1},.dstSubresource={.aspectMask=VK_IMAGE_ASPECT_COLOR_BIT,.layerCount=1},.extent={source->width,source->height,1}};
 vkCmdCopyImage(s->cmd,s->pending_import.image,VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,s->pending_export.image,VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,1,&copy);
 dmabuf_release(vk,&s->pending_import,0);dmabuf_release(vk,&s->pending_export,1);
 if(scene_submit(vk,VK_NULL_HANDLE,VK_NULL_HANDLE,err,errlen))return -1; /* Retain both potentially in-flight resources. */
 PFN_vkGetMemoryFdKHR get_fd=(PFN_vkGetMemoryFdKHR)vkGetDeviceProcAddr(vk->device,"vkGetMemoryFdKHR");
 VkMemoryGetFdInfoKHR get={.sType=VK_STRUCTURE_TYPE_MEMORY_GET_FD_INFO_KHR,.memory=s->pending_export.memory,.handleType=VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT};
 int fd=-1;VkResult r=get_fd?get_fd(vk->device,&get,&fd):VK_ERROR_FEATURE_NOT_PRESENT;
 if(r!=VK_SUCCESS){seterr(err,errlen,"DMA-BUF snapshot export failed",r);goto fail;}
 VkImageSubresource sub={.aspectMask=VK_IMAGE_ASPECT_MEMORY_PLANE_0_BIT_EXT};VkSubresourceLayout layout;
 vkGetImageSubresourceLayout(vk->device,s->pending_export.image,&sub,&layout);
 if(layout.offset>UINT32_MAX||layout.rowPitch>UINT32_MAX){close(fd);seterr(err,errlen,"DMA-BUF snapshot layout exceeds protocol bounds",VK_ERROR_FORMAT_NOT_SUPPORTED);goto fail;}
 *snapshot=(worldr_dmabuf){.width=source->width,.height=source->height,.format=source->format,.stride=(uint32_t)layout.rowPitch,.offset=(uint32_t)layout.offset,.allocation=s->pending_export.allocation,.fd=fd};
 scene_drop_image(vk,&s->pending_import);scene_drop_image(vk,&s->pending_export);return 0;
 fail:scene_drop_image(vk,&s->pending_import);scene_drop_image(vk,&s->pending_export);return -1;
}
int worldr_vk_upload_dmabuf(worldr_vk *vk,uint64_t id,const worldr_dmabuf *source,char *err,int errlen)
{
 if(!id||!worldr_vk_dmabuf_supported(vk)){seterr(err,errlen,"DMA-BUF sharing unsupported",VK_ERROR_FEATURE_NOT_PRESENT);return -1;}
 if(scene_init(vk,err,errlen)||scene_wait(vk,err,errlen))return -1;
 worldr_scene *s=vk->scene;scene_drop_image(vk,&s->pending_import);
 scene_texture **link=scene_texture_link(vk,id);
 if(*link)return 0; /* Immutable external backing: each snapshot has a new ID. */
	if(dmabuf_image(vk,source,1,&s->pending_import,err,errlen))return -1;
	scene_texture *texture=calloc(1,sizeof(*texture));VkResult r=VK_ERROR_OUT_OF_HOST_MEMORY;
	if(!texture)goto allocation_fail;
	texture->id=id;texture->width=source->width;texture->height=source->height;
	VkFormat vk_format=dmabuf_vk_format(source->format);
	if(scene_make_image(vk,&texture->image,source->width,source->height,vk_format,VK_IMAGE_USAGE_SAMPLED_BIT|VK_IMAGE_USAGE_TRANSFER_DST_BIT,VK_IMAGE_ASPECT_COLOR_BIT,err,errlen))goto fail;
	if(source->format==WORLDR_DRM_XRGB||source->format==WORLDR_DRM_XBGR||
	   source->format==WORLDR_DRM_XRGB2101010||source->format==WORLDR_DRM_XBGR2101010){
	 vkDestroyImageView(vk->device,texture->image.view,NULL);texture->image.view=VK_NULL_HANDLE;
	 VkImageViewCreateInfo view={.sType=VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,.image=texture->image.image,.viewType=VK_IMAGE_VIEW_TYPE_2D,.format=vk_format,.components={.a=VK_COMPONENT_SWIZZLE_ONE},.subresourceRange={.aspectMask=VK_IMAGE_ASPECT_COLOR_BIT,.levelCount=1,.layerCount=1}};
  if((r=vkCreateImageView(vk->device,&view,NULL,&texture->image.view))!=VK_SUCCESS)goto allocation_fail;
 }
 VkDescriptorPoolSize size={.type=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.descriptorCount=1};
 VkDescriptorPoolCreateInfo pool={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,.maxSets=1,.poolSizeCount=1,.pPoolSizes=&size};
 if((r=vkCreateDescriptorPool(vk->device,&pool,NULL,&texture->pool))!=VK_SUCCESS)goto allocation_fail;
 VkDescriptorSetAllocateInfo descriptor={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,.descriptorPool=texture->pool,.descriptorSetCount=1,.pSetLayouts=&s->descriptor_layout};
 if((r=vkAllocateDescriptorSets(vk->device,&descriptor,&texture->descriptor))!=VK_SUCCESS)goto allocation_fail;
 VkDescriptorImageInfo image={.sampler=s->sampler,.imageView=texture->image.view,.imageLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL};
 VkWriteDescriptorSet write={.sType=VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,.dstSet=texture->descriptor,.dstBinding=0,.descriptorCount=1,.descriptorType=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.pImageInfo=&image};
 vkUpdateDescriptorSets(vk->device,1,&write,0,NULL);
 if(scene_begin(vk,err,errlen))goto fail;
 dmabuf_acquire(vk,&s->pending_import);
 barrier(s->cmd,texture->image.image,VK_IMAGE_LAYOUT_UNDEFINED,VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,0,VK_ACCESS_TRANSFER_WRITE_BIT,VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT,VK_PIPELINE_STAGE_TRANSFER_BIT);
 VkImageCopy copy={.srcSubresource={.aspectMask=VK_IMAGE_ASPECT_COLOR_BIT,.layerCount=1},.dstSubresource={.aspectMask=VK_IMAGE_ASPECT_COLOR_BIT,.layerCount=1},.extent={source->width,source->height,1}};
 vkCmdCopyImage(s->cmd,s->pending_import.image,VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,texture->image.image,VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,1,&copy);
 dmabuf_release(vk,&s->pending_import,0);
 barrier(s->cmd,texture->image.image,VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,VK_ACCESS_TRANSFER_WRITE_BIT,VK_ACCESS_SHADER_READ_BIT,VK_PIPELINE_STAGE_TRANSFER_BIT,VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT);
 texture->next=*link;*link=texture;
 if(scene_submit(vk,VK_NULL_HANDLE,VK_NULL_HANDLE,err,errlen))return -1;
 scene_drop_image(vk,&s->pending_import);return 0;
 allocation_fail:seterr(err,errlen,"DMA-BUF retained texture allocation failed",r);
 fail:scene_drop_image(vk,&s->pending_import);if(texture)scene_drop_texture(vk,texture);return -1;
}
