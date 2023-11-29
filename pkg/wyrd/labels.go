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

func MergeLabels(labl ...Labels) Labels {
	count := 0
	for _, l := range labl {
		count += len(l)
	}

	result := make(Labels, count)
	for _, l := range labl {
		for k, v := range l {
			result[k] = v
		}
	}

	return result
}

type Selector struct {
	Key    string   `json:"key,omitempty" yaml:"key,omitempty" `
	Op     string   `json:"operator,omitempty" yaml:"operator,omitempty" `
	Values []string `json:"values,omitempty" yaml:"values,omitempty" `
}

// LabelSelector is a part of model that holds label-based requirements for on other resources
type LabelSelector struct {
	MatchLabels Labels `json:"matchLabels,omitempty" yaml:"matchLabels,omitempty" `

	MatchSelector []Selector `json:"matchSelector,omitempty" yaml:"matchSelector,omitempty" `
}

// func (ls LabelSelector) Match(labels Labels) bool {
// 	// selector, err := labels.Parse(ls.MatchLabels)
// 	// if err != nil {
// 	// 	return nil, err
// 	// }

// 	return true
// }
