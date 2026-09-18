//go:build linux && cgo

#include "terminal.h"
#include <vterm.h>
#include <xkbcommon/xkbcommon.h>
#include <xkbcommon/xkbcommon-compose.h>
#include <stdlib.h>
#include <string.h>

#if VTERM_VERSION_MAJOR == 0 && VTERM_VERSION_MINOR < 3
#error "worldr native terminal requires libvterm >= 0.3"
#endif

#define OUTPUT_CAPACITY (2u*1024u*1024u)
#define HISTORY_BYTES (32u*1024u*1024u)
struct history_line { VTermScreenCell *cells; int cols; };
struct worldr_term {
    VTerm *vt;
    VTermScreen *screen;
    VTermState *state;
    struct xkb_context *xkb;
    struct xkb_keymap *keymap;
    struct xkb_state *keys;
    struct xkb_compose_table *compose_table;
    struct xkb_compose_state *compose;
    struct history_line *history;
    int history_capacity, history_head, history_count, scroll_offset;
    size_t history_bytes;
    uint64_t revision, first_line, command_id;
    int rows,cols,cursor_row,cursor_col,cursor_visible,cursor_shape,cursor_blink,mouse,reverse,alternate;
    worldr_term_command commands[128]; int command_count;
    char marker[64]; size_t marker_len; int marker_overflow;
    char title[4097];size_t title_len;
    char *output;size_t output_len;int overflow;
};

static int damage(VTermRect rect,void *data){(void)rect;((worldr_term*)data)->revision++;return 1;}
static int move_rect(VTermRect dest,VTermRect src,void *data){(void)dest;(void)src;((worldr_term*)data)->revision++;return 1;}
static int move_cursor(VTermPos pos,VTermPos old,int visible,void *data){(void)old;worldr_term*t=data;t->cursor_row=pos.row;t->cursor_col=pos.col;t->cursor_visible=visible;t->revision++;return 1;}
static int property(VTermProp prop,VTermValue *value,void *data){
    worldr_term*t=data;
    switch(prop){
    case VTERM_PROP_CURSORVISIBLE:t->cursor_visible=value->boolean;break;
    case VTERM_PROP_CURSORBLINK:t->cursor_blink=value->boolean;break;
    case VTERM_PROP_CURSORSHAPE:t->cursor_shape=value->number;break;
    case VTERM_PROP_MOUSE:t->mouse=value->number;break;
    case VTERM_PROP_REVERSE:t->reverse=value->boolean;break;
    case VTERM_PROP_TITLE:{
        VTermStringFragment text=value->string;
        if(text.initial)t->title_len=0;
        size_t n=text.len;if(n>4096-t->title_len)n=4096-t->title_len;
        if(n)memcpy(t->title+t->title_len,text.str,n);
        t->title_len+=n;t->title[t->title_len]=0;
        break;
    }
    case VTERM_PROP_ALTSCREEN:t->scroll_offset=0;t->alternate=value->boolean;break;
    default:break;
    }
    t->revision++;return 1;
}
static int bell(void *data){(void)data;return 1;}
static int resized(int rows,int cols,void *data){worldr_term*t=data;t->rows=rows;t->cols=cols;t->revision++;return 1;}
static void discard_oldest(worldr_term*t){
    if(!t->history_count)return;
    struct history_line *line=&t->history[t->history_head];
    t->history_bytes-=(size_t)line->cols*sizeof(*line->cells);free(line->cells);memset(line,0,sizeof(*line));
    t->history_head=(t->history_head+1)%t->history_capacity;t->history_count--;t->first_line++;
}
static int push_line(int cols,const VTermScreenCell *cells,void *data){
    worldr_term*t=data;
    if(!t->history_capacity){t->first_line++;return 1;}
    size_t bytes=(size_t)cols*sizeof(*cells);
    while(t->history_count && (t->history_count==t->history_capacity || t->history_bytes+bytes>HISTORY_BYTES))discard_oldest(t);
    if(bytes>HISTORY_BYTES)return 1;
    VTermScreenCell *copy=malloc(bytes);if(!copy)return 0;memcpy(copy,cells,bytes);
    int index=(t->history_head+t->history_count)%t->history_capacity;
    t->history[index]=(struct history_line){.cells=copy,.cols=cols};t->history_count++;t->history_bytes+=bytes;
    if(t->scroll_offset)t->scroll_offset++;
    if(t->scroll_offset>t->history_count)t->scroll_offset=t->history_count;
    t->revision++;return 1;
}
static void blank_cell(worldr_term*t,VTermScreenCell*cell){
    memset(cell,0,sizeof(*cell));cell->width=1;
    vterm_state_get_default_colors(t->state,&cell->fg,&cell->bg);
}
static int pop_line(int cols,VTermScreenCell*cells,void *data){
    worldr_term*t=data;if(!t->history_count)return 0;
    int index=(t->history_head+t->history_count-1)%t->history_capacity;
    struct history_line *line=&t->history[index];
    for(int i=0;i<cols;i++){if(i<line->cols)cells[i]=line->cells[i];else blank_cell(t,&cells[i]);}
    t->history_bytes-=(size_t)line->cols*sizeof(*cells);free(line->cells);memset(line,0,sizeof(*line));t->history_count--;
    if(t->scroll_offset>t->history_count)t->scroll_offset=t->history_count;
    t->revision++;return 1;
}
static int clear_history(void *data){worldr_term*t=data;while(t->history_count)discard_oldest(t);t->scroll_offset=0;t->revision++;return 1;}
static const VTermScreenCallbacks callbacks={.damage=damage,.moverect=move_rect,.movecursor=move_cursor,.settermprop=property,.bell=bell,.resize=resized,.sb_pushline=push_line,.sb_popline=pop_line,.sb_clear=clear_history};
// OSC 133 is opt-in shell metadata, never executable input. Fragment assembly
// and command retention are bounded independently of untrusted PTY output.
static int shell_marker(int command,VTermStringFragment frag,void *data){
    worldr_term*t=data;if(command!=133)return 0;
    if(frag.initial){t->marker_len=0;t->marker_overflow=0;}
    if(frag.len>=sizeof(t->marker)-t->marker_len)t->marker_overflow=1;
    else if(!t->marker_overflow){memcpy(t->marker+t->marker_len,frag.str,frag.len);t->marker_len+=frag.len;}
    if(!frag.final||t->marker_overflow||t->alternate)return 1;
    t->marker[t->marker_len]=0;
    uint64_t line=t->first_line+(uint64_t)t->history_count+(uint64_t)t->cursor_row;
    if(!strcmp(t->marker,"B")){
        if(t->command_count==128){memmove(t->commands,t->commands+1,127*sizeof(*t->commands));t->command_count--;}
        t->commands[t->command_count++]=(worldr_term_command){.id=++t->command_id,.command_line=line,.command_col=t->cursor_col,.status=-1};
    }else if(t->command_count){
        worldr_term_command*c=&t->commands[t->command_count-1];
        if(!strcmp(t->marker,"C")&&!c->started&&!c->finished){c->output_line=line;c->output_col=t->cursor_col;c->started=1;}
        else if(t->marker[0]=='D'&&(t->marker[1]==0||t->marker[1]==';')&&c->started&&!c->finished){
            c->end_line=line;c->end_col=t->cursor_col;c->finished=1;
            if(t->marker[1]==';'){char*end=NULL;long code=strtol(t->marker+2,&end,10);if(end!=t->marker+2&&*end==0&&code>=0&&code<=255)c->status=(int)code;}
        }
    }
    t->revision++;return 1;
}
static const VTermStateFallbacks fallbacks={.osc=shell_marker};
static void output(const char *text,size_t length,void *data){
    worldr_term*t=data;
    if(length>OUTPUT_CAPACITY-t->output_len){t->overflow=1;return;}
    memcpy(t->output+t->output_len,text,length);t->output_len+=length;
}
worldr_term *worldr_term_new(int rows,int cols,int scrollback){
    worldr_term*t=calloc(1,sizeof(*t));if(!t)return NULL;
    t->rows=rows;t->cols=cols;t->cursor_shape=1;t->cursor_visible=1;t->cursor_blink=1;t->revision=1;
    t->history_capacity=scrollback;t->history=calloc(scrollback?scrollback:1,sizeof(*t->history));t->output=malloc(OUTPUT_CAPACITY);
    if(!t->history || !t->output)goto fail;
    t->vt=vterm_new(rows,cols);if(!t->vt)goto fail;
    vterm_set_utf8(t->vt,1);vterm_output_set_callback(t->vt,output,t);
    t->state=vterm_obtain_state(t->vt);t->screen=vterm_obtain_screen(t->vt);
    vterm_screen_set_callbacks(t->screen,&callbacks,t);vterm_screen_enable_altscreen(t->screen,1);vterm_screen_set_damage_merge(t->screen,VTERM_DAMAGE_ROW);
    vterm_screen_set_unrecognised_fallbacks(t->screen,&fallbacks,t);
    VTermColor fg,bg;vterm_color_rgb(&fg,220,235,238);vterm_color_rgb(&bg,8,16,23);vterm_state_set_default_colors(t->state,&fg,&bg);
    vterm_screen_reset(t->screen,1);
    t->xkb=xkb_context_new(XKB_CONTEXT_NO_FLAGS);if(!t->xkb)goto fail;
    struct xkb_rule_names names={.layout="us"};t->keymap=xkb_keymap_new_from_names(t->xkb,&names,XKB_KEYMAP_COMPILE_NO_FLAGS);if(!t->keymap)goto fail;
    t->keys=xkb_state_new(t->keymap);if(!t->keys)goto fail;
    const char *locale=getenv("LC_ALL");if(!locale||!*locale)locale=getenv("LC_CTYPE");if(!locale||!*locale)locale=getenv("LANG");if(!locale||!*locale)locale="C.UTF-8";
    t->compose_table=xkb_compose_table_new_from_locale(t->xkb,locale,XKB_COMPOSE_COMPILE_NO_FLAGS);
    if(t->compose_table)t->compose=xkb_compose_state_new(t->compose_table,XKB_COMPOSE_STATE_NO_FLAGS);
    return t;
fail:worldr_term_free(t);return NULL;
}
void worldr_term_free(worldr_term*t){if(!t)return;clear_history(t);free(t->history);free(t->output);if(t->vt)vterm_free(t->vt);xkb_compose_state_unref(t->compose);xkb_compose_table_unref(t->compose_table);xkb_state_unref(t->keys);xkb_keymap_unref(t->keymap);xkb_context_unref(t->xkb);free(t);}
int worldr_term_feed(worldr_term*t,const char*data,size_t len){size_t used=vterm_input_write(t->vt,data,len);vterm_screen_flush_damage(t->screen);return used==len?0:-1;}
void worldr_term_resize(worldr_term*t,int rows,int cols){vterm_set_size(t->vt,rows,cols);vterm_screen_flush_damage(t->screen);t->rows=rows;t->cols=cols;t->revision++;}
worldr_term_info worldr_term_snapshot(worldr_term*t,worldr_term_cell*cells){
    worldr_term_info info={.revision=t->revision,.first_line=t->first_line,.rows=t->rows,.cols=t->cols,.cursor_row=t->cursor_row,.cursor_col=t->cursor_col,.cursor_visible=t->cursor_visible&&!t->scroll_offset,.cursor_shape=t->cursor_shape,.cursor_blink=t->cursor_blink,.scroll_offset=t->scroll_offset,.scrollback_len=t->history_count,.mouse=t->mouse,.alternate=t->alternate};
    if(!cells)return info;
    for(int row=0;row<t->rows;row++)for(int col=0;col<t->cols;col++){
        VTermScreenCell cell;blank_cell(t,&cell);
        int index=t->history_count-t->scroll_offset+row;
        if(index<t->history_count){struct history_line *line=&t->history[(t->history_head+index)%t->history_capacity];if(col<line->cols)cell=line->cells[col];}
        else vterm_screen_get_cell(t->screen,(VTermPos){.row=index-t->history_count,.col=col},&cell);
        worldr_term_cell*out=&cells[row*t->cols+col];memset(out,0,sizeof(*out));
        for(int i=0;i<6;i++)out->chars[i]=cell.chars[i]<=0x10ffff&&!cell.attrs.conceal?cell.chars[i]:0;
        out->width=cell.chars[0]==UINT32_MAX?0:cell.width;out->bold=cell.attrs.bold;out->italic=cell.attrs.italic;out->underline=cell.attrs.underline;out->reverse=cell.attrs.reverse^t->reverse;out->strike=cell.attrs.strike;
        vterm_screen_convert_color_to_rgb(t->screen,&cell.fg);vterm_screen_convert_color_to_rgb(t->screen,&cell.bg);
        out->fg[0]=cell.fg.rgb.red;out->fg[1]=cell.fg.rgb.green;out->fg[2]=cell.fg.rgb.blue;
        out->bg[0]=cell.bg.rgb.red;out->bg[1]=cell.bg.rgb.green;out->bg[2]=cell.bg.rgb.blue;
    }
    return info;
}
const char *worldr_term_title(worldr_term*t){return t->title;}
size_t worldr_term_output(worldr_term*t,char*data,size_t capacity){size_t n=t->output_len;if(n>capacity)n=capacity;memcpy(data,t->output,n);memmove(t->output,t->output+n,t->output_len-n);t->output_len-=n;return n;}
int worldr_term_overflow(worldr_term*t){int overflow=t->overflow;t->overflow=0;return overflow;}
int worldr_term_keymap(worldr_term*t,const char*map){struct xkb_keymap*k=xkb_keymap_new_from_string(t->xkb,map,XKB_KEYMAP_FORMAT_TEXT_V1,XKB_KEYMAP_COMPILE_NO_FLAGS);if(!k)return -1;struct xkb_state*s=xkb_state_new(k);if(!s){xkb_keymap_unref(k);return -1;}xkb_state_unref(t->keys);xkb_keymap_unref(t->keymap);t->keys=s;t->keymap=k;if(t->compose)xkb_compose_state_reset(t->compose);return 0;}
void worldr_term_modifiers(worldr_term*t,uint32_t depressed,uint32_t latched,uint32_t locked,uint32_t group){xkb_state_update_mask(t->keys,depressed,latched,locked,0,0,group);}
static VTermModifier key_modifiers(worldr_term*t){int mod=0;if(xkb_state_mod_name_is_active(t->keys,XKB_MOD_NAME_SHIFT,XKB_STATE_MODS_EFFECTIVE)>0)mod|=VTERM_MOD_SHIFT;if(xkb_state_mod_name_is_active(t->keys,XKB_MOD_NAME_CTRL,XKB_STATE_MODS_EFFECTIVE)>0)mod|=VTERM_MOD_CTRL;if(xkb_state_mod_name_is_active(t->keys,XKB_MOD_NAME_ALT,XKB_STATE_MODS_EFFECTIVE)>0)mod|=VTERM_MOD_ALT;return mod;}
void worldr_term_key(worldr_term*t,uint32_t code){
    xkb_keysym_t sym=xkb_state_key_get_one_sym(t->keys,code+8);VTermModifier mod=key_modifiers(t);VTermKey key=VTERM_KEY_NONE;
    switch(sym){
    case XKB_KEY_Return:key=VTERM_KEY_ENTER;break;case XKB_KEY_KP_Enter:key=VTERM_KEY_KP_ENTER;break;
    case XKB_KEY_Tab:key=VTERM_KEY_TAB;break;case XKB_KEY_ISO_Left_Tab:key=VTERM_KEY_TAB;mod|=VTERM_MOD_SHIFT;break;
    case XKB_KEY_BackSpace:key=VTERM_KEY_BACKSPACE;break;case XKB_KEY_Escape:key=VTERM_KEY_ESCAPE;break;
    case XKB_KEY_Up:key=VTERM_KEY_UP;break;case XKB_KEY_Down:key=VTERM_KEY_DOWN;break;case XKB_KEY_Left:key=VTERM_KEY_LEFT;break;case XKB_KEY_Right:key=VTERM_KEY_RIGHT;break;
    case XKB_KEY_Insert:key=VTERM_KEY_INS;break;case XKB_KEY_Delete:key=VTERM_KEY_DEL;break;case XKB_KEY_Home:key=VTERM_KEY_HOME;break;case XKB_KEY_End:key=VTERM_KEY_END;break;case XKB_KEY_Page_Up:key=VTERM_KEY_PAGEUP;break;case XKB_KEY_Page_Down:key=VTERM_KEY_PAGEDOWN;break;
    case XKB_KEY_KP_Insert:key=VTERM_KEY_INS;break;case XKB_KEY_KP_Delete:key=VTERM_KEY_DEL;break;case XKB_KEY_KP_Home:key=VTERM_KEY_HOME;break;case XKB_KEY_KP_End:key=VTERM_KEY_END;break;case XKB_KEY_KP_Page_Up:key=VTERM_KEY_PAGEUP;break;case XKB_KEY_KP_Page_Down:key=VTERM_KEY_PAGEDOWN;break;
    case XKB_KEY_KP_Up:key=VTERM_KEY_UP;break;case XKB_KEY_KP_Down:key=VTERM_KEY_DOWN;break;case XKB_KEY_KP_Left:key=VTERM_KEY_LEFT;break;case XKB_KEY_KP_Right:key=VTERM_KEY_RIGHT;break;
    case XKB_KEY_KP_Decimal:key=VTERM_KEY_KP_PERIOD;break;case XKB_KEY_KP_Add:key=VTERM_KEY_KP_PLUS;break;case XKB_KEY_KP_Subtract:key=VTERM_KEY_KP_MINUS;break;case XKB_KEY_KP_Multiply:key=VTERM_KEY_KP_MULT;break;case XKB_KEY_KP_Divide:key=VTERM_KEY_KP_DIVIDE;break;case XKB_KEY_KP_Equal:key=VTERM_KEY_KP_EQUAL;break;case XKB_KEY_KP_Separator:key=VTERM_KEY_KP_COMMA;break;
    default:if(sym>=XKB_KEY_F1&&sym<=XKB_KEY_F35)key=VTERM_KEY_FUNCTION(sym-XKB_KEY_F1+1);else if(sym>=XKB_KEY_KP_0&&sym<=XKB_KEY_KP_9)key=VTERM_KEY_KP_0+sym-XKB_KEY_KP_0;break;
    }
    if(key!=VTERM_KEY_NONE){if(t->compose)xkb_compose_state_reset(t->compose);vterm_keyboard_key(t->vt,key,mod);return;}
    if(t->compose && !(mod&(VTERM_MOD_ALT|VTERM_MOD_CTRL))){
        xkb_compose_state_feed(t->compose,sym);
        switch(xkb_compose_state_get_status(t->compose)){
        case XKB_COMPOSE_COMPOSING:return;
        case XKB_COMPOSE_COMPOSED:{char text[128];int n=xkb_compose_state_get_utf8(t->compose,text,sizeof(text));xkb_compose_state_reset(t->compose);if(n>0&&n<(int)sizeof(text))output(text,n,t);return;}
        case XKB_COMPOSE_CANCELLED:xkb_compose_state_reset(t->compose);return;
        default:break;
        }
    }
    uint32_t rune=xkb_keysym_to_utf32(sym);if(rune)vterm_keyboard_unichar(t->vt,rune,mod);
}
int worldr_term_repeats(worldr_term*t,uint32_t code){return xkb_keymap_key_repeats(t->keymap,code+8);}
void worldr_term_focus(worldr_term*t,int focused){if(focused)vterm_state_focus_in(t->state);else{for(int button=1;button<=3;button++)vterm_mouse_button(t->vt,button,0,VTERM_MOD_NONE);vterm_state_focus_out(t->state);if(t->compose)xkb_compose_state_reset(t->compose);}t->revision++;}
void worldr_term_paste(worldr_term*t,const char*data,size_t len){vterm_keyboard_start_paste(t->vt);output(data,len,t);vterm_keyboard_end_paste(t->vt);}
void worldr_term_scroll(worldr_term*t,int lines){if(t->alternate)return;long next=(long)t->scroll_offset+lines;if(next<0)next=0;if(next>t->history_count)next=t->history_count;if(next!=t->scroll_offset){t->scroll_offset=(int)next;t->revision++;}}
void worldr_term_mouse(worldr_term*t,int row,int col,int button,int pressed,int mods){vterm_mouse_move(t->vt,row,col,mods);if(button)vterm_mouse_button(t->vt,button,pressed,mods);}
void worldr_term_mouse_cancel(worldr_term*t){for(int button=1;button<=3;button++)vterm_mouse_button(t->vt,button,0,VTERM_MOD_NONE);}
int worldr_term_text_line(worldr_term*t,int index,worldr_term_text_cell*cells,int capacity){
    if(index<0||index>=t->history_count+(t->alternate?0:t->rows))return 0;
    struct history_line*line=index<t->history_count?&t->history[(t->history_head+index)%t->history_capacity]:NULL;
    int cols=line?line->cols:t->cols;if(cols>capacity)cols=capacity;
    for(int col=0;col<cols;col++){
        VTermScreenCell cell;blank_cell(t,&cell);
        if(line)cell=line->cells[col];else vterm_screen_get_cell(t->screen,(VTermPos){.row=index-t->history_count,.col=col},&cell);
        memset(&cells[col],0,sizeof(cells[col]));cells[col].width=cell.chars[0]==UINT32_MAX?0:cell.width;
        for(int j=0;j<6;j++)cells[col].chars[j]=cell.chars[j]<=0x10ffff&&!cell.attrs.conceal?cell.chars[j]:0;
    }
    return cols;
}
int worldr_term_commands(worldr_term*t,worldr_term_command*commands,int capacity){int n=t->command_count;if(n>capacity)n=capacity;memcpy(commands,t->commands,(size_t)n*sizeof(*commands));return n;}
