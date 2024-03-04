package wyrd

import (
	"fmt"
	"strconv"
)

// Type to represent an ID of a resource
type ResourceID uint

// Version ID type
type Version uint64

func (v Version) String() string {
	return strconv.FormatUint(uint64(v), 10)
}

const InvalidResourceID ResourceID = 0

func (r ResourceID) String() string {
	return strconv.FormatInt(int64(r), 10)
}

type VersionedResourceId struct {
	ID      ResourceID `form:"id" json:"id" yaml:"id" xml:"id"`
	Version Version    `form:"version" json:"version" yaml:"version" xml:"version"`
}

func NewVersionedId(id ResourceID, version Version) VersionedResourceId {
	return VersionedResourceId{
		ID:      id,
		Version: version,
	}
}

func (r VersionedResourceId) String() string {
	return fmt.Sprintf("%v@%d", r.ID, r.Version)
}

type Resourceable interface {
	GetID() ResourceID
	GetVersionedID() VersionedResourceId
	IsDeleted() bool
}
