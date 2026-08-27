package config

import "strings"

// Action names an operation the UI can perform. Bindings map key sequences to
// these, so config refers to intent rather than to internals.
type Action string

// Navigation actions. These are the only ones bound to bare single letters:
// on a list where a count or motion may be half-typed, a bare key that opens
// an editor or mutates an item is an accident waiting to happen.
const (
	ActionDown         Action = "down"
	ActionUp           Action = "up"
	ActionTop          Action = "top"
	ActionBottom       Action = "bottom"
	ActionHalfPageDown Action = "half_page_down"
	ActionHalfPageUp   Action = "half_page_up"
	ActionViewportTop  Action = "viewport_top"
	ActionViewportMid  Action = "viewport_middle"
	ActionViewportBot  Action = "viewport_bottom"

	ActionOpen     Action = "open"
	ActionJumpBack Action = "jump_back"
	ActionJumpFwd  Action = "jump_forward"

	ActionPaneLeft  Action = "pane_left"
	ActionPaneRight Action = "pane_right"
	ActionPaneZoom  Action = "pane_zoom"

	// Pane and detail-tab navigation. These keep Tab free for text input.
	ActionGoList   Action = "go_list"
	ActionGoDetail Action = "go_detail"
	ActionPrevTab  Action = "previous_tab"
	ActionNextTab  Action = "next_tab"

	ActionSearchInPane Action = "search_in_pane"
	ActionSearchNext   Action = "search_next"
	ActionSearchPrev   Action = "search_prev"

	ActionCommandline Action = "commandline"
	ActionNormalMode  Action = "normal_mode"
	ActionClosePane   Action = "close_pane"
	ActionQuit        Action = "quit"
	ActionReload      Action = "reload"
	ActionHelp        Action = "help"
)

// Leader-prefixed actions. Everything that mutates, opens an editor, or leaves
// the application lives here.
const (
	ActionComment     Action = "comment"
	ActionEditDesc    Action = "edit_description"
	ActionTransition  Action = "transition"
	ActionAssign      Action = "assign"
	ActionYankKey     Action = "yank_key"
	ActionYankURL     Action = "yank_url"
	ActionOpenBrowser Action = "open_browser"
)

// transitionPrefix marks an action that applies one named workflow transition
// directly, without opening the picker. The name travels inside the action
// because it is not knowable at compile time whether the site has such a
// transition -- and whether it is available depends on the item and the moment,
// so it can only be resolved live at the keypress.
const transitionPrefix = "transition:"

// TransitionAction names the action that applies the named transition.
func TransitionAction(name string) Action { return Action(transitionPrefix + name) }

// TransitionName reports the transition an action applies directly, and false
// for every fixed action.
func (a Action) TransitionName() (string, bool) {
	return strings.CutPrefix(string(a), transitionPrefix)
}

// Keymap binds key sequences to actions.
type Keymap map[Action]string

// DefaultKeymap is the built-in binding set. User config merges over it.
func DefaultKeymap() Keymap {
	return Keymap{
		ActionDown:         "j",
		ActionUp:           "k",
		ActionTop:          "gg",
		ActionBottom:       "G",
		ActionHalfPageDown: "ctrl+d",
		ActionHalfPageUp:   "ctrl+u",
		ActionViewportTop:  "H",
		ActionViewportMid:  "M",
		ActionViewportBot:  "L",

		ActionOpen:     "enter",
		ActionJumpBack: "ctrl+o",
		ActionJumpFwd:  "ctrl+i",

		ActionPaneLeft:  "ctrl+w h",
		ActionPaneRight: "ctrl+w l",
		ActionPaneZoom:  "ctrl+w o",

		ActionGoList:   "gl",
		ActionGoDetail: "gd",
		ActionPrevTab:  "[",
		ActionNextTab:  "]",

		ActionSearchInPane: "/",
		ActionSearchNext:   "n",
		ActionSearchPrev:   "N",

		ActionCommandline: ":",
		ActionNormalMode:  "esc",
		ActionClosePane:   "q",
		ActionQuit:        "Q",
		ActionReload:      "R",
		ActionHelp:        "?",

		ActionComment:     "<leader>c",
		ActionEditDesc:    "<leader>e",
		ActionTransition:  "<leader>t",
		ActionAssign:      "<leader>a",
		ActionYankKey:     "<leader>y",
		ActionYankURL:     "<leader>Y",
		ActionOpenBrowser: "<leader>o",
	}
}

// Merge overlays user bindings onto m. An empty binding unbinds its action.
// The receiver is not modified, so DefaultKeymap stays the thing it says it is.
func (m Keymap) Merge(user map[string]string) Keymap {
	out := make(Keymap, len(m))
	for a, b := range m {
		out[a] = b
	}
	for name, binding := range user {
		a := Action(name)
		if binding == "" {
			delete(out, a)
			continue
		}
		out[a] = binding
	}
	return out
}

// IsAction reports whether name is a binding target jt knows about. Config
// naming an unknown action is a typo the user wants told about, not a binding
// that silently never fires.
func IsAction(name string) bool {
	_, ok := DefaultKeymap()[Action(name)]
	return ok
}
