package wyrd

// Same as 	"k8s.io/apimachinery/pkg/labels".Set
type Labels map[string]string

func (l Labels) Has(key string) bool {
	_, ok := l[key]
	return ok
}

func (l Labels) Get(key string) string {
	return l[key]
}

func MergeLabels(l, r Labels) Labels {
	result := make(Labels, len(l)+len(r))

	for k, v := range l {
		result[k] = v
	}

	for k, v := range r {
		result[k] = v
	}

	return result
}

type Selector struct {
	Key    string
	Op     string
	Values []string
}

// LabelSelector is a part of model that holds label-based requirements for on other resources
type LabelSelector struct {
	MatchLabels Labels `json:"matchLabels,omitempty" yaml:"matchLabels,omitempty" `

	MatchSelector []Selector `json:",omitempty" yaml:",omitempty" `
}

// func (ls LabelSelector) Match(labels Labels) bool {
// 	// selector, err := labels.Parse(ls.MatchLabels)
// 	// if err != nil {
// 	// 	return nil, err
// 	// }

// 	return true
// }
