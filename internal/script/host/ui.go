package host

import "abidata/internal/catalog"

// UISession is the state of one form event (brief <forms> event protocol):
// inputs from the client and the outputs the handler produces.
type UISession struct {
	// Inputs.
	Table         *catalog.Table // the form's record source table (nil for unbound forms)
	Event         string
	Control       string
	OldValue      any
	NewValue      any
	Args          map[string]any
	Record        map[string]any // field (physical name) → value in Go form (decimals as strings, NULL as nil)
	RecordID      int64
	RecordVersion int64
	IsNew         bool
	Dirty         map[string]bool
	Controls      map[string]map[string]any // control name → current props
	Continuation  string                    // set when re-invoked after ui.confirm
	Answer        *bool

	// Outputs.
	Changed       map[string]any // fields the handler assigned
	Patches       []Patch
	Notifications []Notification
	Cancel        *string
	Navigate      map[string]any
	Confirm       *Confirm
	Focus         string
	Requery       []string
	Close         bool
}

// Patch sets control properties.
type Patch struct {
	Op      string         `json:"op"`
	Control string         `json:"control"`
	Props   map[string]any `json:"props"`
}

// Notification is a toast or alert.
type Notification struct {
	Kind  string `json:"kind" enum:"toast,alert"`
	Level string `json:"level" enum:"info,success,warning,error"`
	Text  string `json:"text"`
	Title string `json:"title,omitempty"`
}

// Confirm asks the client to confirm and re-invoke with the continuation.
type Confirm struct {
	Message      string `json:"message"`
	Continuation string `json:"continuation"`
}

// SetField records a field assignment from the handler.
func (s *UISession) SetField(name string, v any) {
	if s.Changed == nil {
		s.Changed = map[string]any{}
	}
	s.Changed[name] = v
	if s.Record == nil {
		s.Record = map[string]any{}
	}
	s.Record[name] = v
}

// Notify appends a notification (bounded to avoid unbounded output).
func (s *UISession) Notify(n Notification) {
	if len(s.Notifications) < 50 {
		s.Notifications = append(s.Notifications, n)
	}
}

// Patch appends a control patch, merging consecutive patches to the same control.
func (s *UISession) Patch(control string, props map[string]any) {
	for i := range s.Patches {
		if s.Patches[i].Control == control {
			for k, v := range props {
				s.Patches[i].Props[k] = v
			}
			return
		}
	}
	if len(s.Patches) < 500 {
		s.Patches = append(s.Patches, Patch{Op: "set", Control: control, Props: props})
	}
	if s.Controls != nil {
		if cur, ok := s.Controls[control]; ok {
			for k, v := range props {
				cur[k] = v
			}
		}
	}
}
