static struct app_content content_capture(struct app_surface *v) {
  struct app_content c = {.image = v->image,
                          .scale = v->scale,
                          .viewport_source = v->viewport_source,
                          .viewport_destination = v->viewport_destination,
                          .geometry_x = v->geometry_x,
                          .geometry_y = v->geometry_y,
                          .geometry_width = v->geometry_width,
                          .geometry_height = v->geometry_height};
  memcpy(c.viewport_src, v->viewport_src, sizeof(c.viewport_src));
  memcpy(c.viewport_dst, v->viewport_dst, sizeof(c.viewport_dst));
  c.input = v->input;
  return c;
}
// Transfers the supplied image reference; the caller handles the old reference.
static void content_install(struct app_surface *v, struct app_content c) {
  v->image = NULL;
  image_assign(v, c.image);
  v->scale = c.scale;
  v->viewport_source = c.viewport_source;
  v->viewport_destination = c.viewport_destination;
  memcpy(v->viewport_src, c.viewport_src, sizeof(v->viewport_src));
  memcpy(v->viewport_dst, c.viewport_dst, sizeof(v->viewport_dst));
  v->geometry_x = c.geometry_x;
  v->geometry_y = c.geometry_y;
  v->geometry_width = c.geometry_width;
  v->geometry_height = c.geometry_height;
  v->input = c.input;
}
static int effectively_synchronized(struct app_surface *v) {
  for (; v && v->subsurface; v = v->parent)
    if (v->synchronized)
      return 1;
  return 0;
}
static void apply_unmap(struct app_surface *v) {
  struct worldr_apps *s = v->server;
  if (s->drag_origin && descendant(s->drag_origin, v))
    drag_cancel(s, 1);
  else if (s->drag_target && descendant(s->drag_target, v)) {
    if (s->drag_active)
      drag_motion(s, NULL, 0, 0);
    else
      drag_cancel(s, 1);
  }
  dismiss_surface_tree(v);
  if (s->popup_grab && descendant(s->popup_grab, v))
    dismiss_popups(s);
}
static void apply_update(struct app_surface *v, struct app_update *update) {
  if (!update || update->generation <= v->applied_generation)
    return;
  int mapped = has_buffer(v);
  image_release(v->image);
  image_retain(update->content.image);
  content_install(v, update->content);
  v->applied_generation = update->generation;
  // Child membership, position and stacking belong to this parent update,
  // including when this update itself was cached by a synchronized ancestor.
  struct app_surface *child;
  wl_list_for_each(child, &v->server->surfaces, link) {
    if (child->parent != v || !child->subsurface)
      continue;
    child->placed = 0;
    for (int i = 0; i < update->child_count; i++) {
      if (update->children[i].id != child->id ||
          update->children[i].association != child->association)
        continue;
      child->x = update->children[i].x;
      child->y = update->children[i].y;
      child->below_parent = update->children[i].below;
      child->stack_order = i;
      child->placed = 1;
      apply_update(child, update->children[i].update);
      break;
    }
  }
  if (mapped && !has_buffer(v))
    apply_unmap(v);
  if (v->popup && v->parent) {
    v->x = v->parent->geometry_x + v->popup_x - v->geometry_x;
    v->y = v->parent->geometry_y + v->popup_y - v->geometry_y;
  }
  // Popup coordinates are relative to the parent's window geometry. Moving
  // that geometry origin moves every direct popup even when its protocol
  // configure rectangle remains unchanged (and therefore needs no configure).
  struct app_surface *popup_child;
  wl_list_for_each(popup_child, &v->server->surfaces, link) {
    if (popup_child->parent != v || !popup_child->popup)
      continue;
    popup_child->x =
        v->geometry_x + popup_child->popup_x - popup_child->geometry_x;
    popup_child->y =
        v->geometry_y + popup_child->popup_y - popup_child->geometry_y;
  }
  reactive_popups_changed(v);
  v->snapshot_dirty = 1;
  v->revision++;
  if (v->server->cursor_surface == v || v->server->drag_icon == v)
    v->server->cursor_revision++;
  finish_frames(v, update->generation);
  if (v->popup && has_buffer(v) && v->popup_grabbed && !v->popup_dismissed &&
      v->server->popup_grab == v)
    focus_surface(v->server, v);
}
static struct app_update *create_update(struct app_surface *v) {
  struct worldr_apps *s = v->server;
  if (s->update_count >= 512) {
    post_oom(v->resource);
    return NULL;
  }
  struct app_update *update = calloc(1, sizeof(*update));
  if (!update) {
    post_oom(v->resource);
    return NULL;
  }
  // Build against the last committed state, not the older displayed state.
  // This lets repeated cached commits merge without modifying a parent's
  // already captured dependency graph.
  struct app_content visible = content_capture(v);
  struct app_content basis = v->latest ? v->latest->content : visible;
  image_retain(basis.image);
  content_install(v, basis);
  if (v->pending_attach) {
    if (v->pending_buffer) {
      struct wl_resource *buffer = v->pending_buffer;
      if (copy_buffer(v, buffer) < 0)
        goto fail;
      wl_buffer_send_release(buffer);
      clear_pending_buffer(v);
    } else
      image_assign(v, NULL);
  }
  if (scale_commit(v) < 0)
    goto fail;
  v->scale = v->pending_scale;
  v->geometry_x = v->pending_geometry_x;
  v->geometry_y = v->pending_geometry_y;
  v->geometry_width = v->pending_geometry_width;
  v->geometry_height = v->pending_geometry_height;
  v->input = v->pending_input;
  update->server = s;
  update->refs = 1;
  update->generation = ++s->next_update;
  update->content = content_capture(v);
  s->update_count++;
  content_install(v, visible);
  struct app_surface *child;
  wl_list_for_each(child, &s->surfaces, link) {
    if (child->parent != v || !child->subsurface)
      continue;
    int n = update->child_count++;
    update->children[n].id = child->id;
    update->children[n].association = child->association;
    update->children[n].x = child->pending_x;
    update->children[n].y = child->pending_y;
    update->children[n].below = child->pending_below_parent;
    if (effectively_synchronized(child) && child->latest) {
      update->children[n].update = child->latest;
      child->latest->refs++;
    }
  }
  struct frame_callback *frame;
  wl_list_for_each(frame, &v->frames, link) if (!frame->generation)
      frame->generation = update->generation;
  return update;
fail:
  image_release(v->image);
  content_install(v, visible);
  free(update);
  return NULL;
}
static int ordered_children(struct app_surface *parent,
                            struct app_surface **children) {
  int count = 0;
  struct app_surface *v;
  wl_list_for_each(v, &parent->server->surfaces, link) {
    if (v->parent != parent || (v->subsurface && !v->placed) ||
        !has_buffer(v) || v->popup_dismissed)
      continue;
    int i = count++;
    int rank = v->popup ? MAX_SURFACES + (int)v->id : v->stack_order;
    while (i > 0) {
      struct app_surface *previous = children[i - 1];
      int before = previous->popup ? MAX_SURFACES + (int)previous->id
                                   : previous->stack_order;
      if (before <= rank)
        break;
      children[i] = previous;
      i--;
    }
    children[i] = v;
  }
  return count;
}
