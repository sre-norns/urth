package wyrd

import (
	"fmt"
	"strings"
)

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

type LabelSelectorOperator string

const (
	LabelSelectorOpIn           LabelSelectorOperator = "In"
	LabelSelectorOpNotIn        LabelSelectorOperator = "NotIn"
	LabelSelectorOpExists       LabelSelectorOperator = "Exists"
	LabelSelectorOpDoesNotExist LabelSelectorOperator = "DoesNotExist"
)

type Selector struct {
	Key    string                `json:"key,omitempty" yaml:"key,omitempty" `
	Op     LabelSelectorOperator `json:"operator,omitempty" yaml:"operator,omitempty" `
	Values []string              `json:"values,omitempty" yaml:"values,omitempty" `
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

func (ls LabelSelector) AsLabels() (string, error) {
	sb := strings.Builder{}

	i := 0
	for key, value := range ls.MatchLabels {
		if i != 0 {
			sb.WriteRune(',')
		}
		sb.WriteString(key)
		sb.WriteString("=")
		sb.WriteString(value)
		i += 1
	}

	for _, s := range ls.MatchSelector {
		if sb.Len() > 0 {
			sb.WriteRune(',')
		}
		switch s.Op {
		case LabelSelectorOpExists:
			sb.WriteString(s.Key)
		case LabelSelectorOpDoesNotExist:
			sb.WriteString("!")
			sb.WriteString(s.Key)
		case LabelSelectorOpIn:
			sb.WriteString(s.Key)
			sb.WriteString(" in (")
			for _, value := range s.Values {
				sb.WriteString(value)
			}
			sb.WriteString(")")
		case LabelSelectorOpNotIn:
			sb.WriteString(s.Key)
			sb.WriteString(" notin (")
			for i, value := range s.Values {
				if i != 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(value)
			}
			sb.WriteString(")")
		default:
			return sb.String(), fmt.Errorf("unsupported op value: %q", s.Op)
		}
	}

	return sb.String(), nil
}
