package dbstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/sre-norns/urth/pkg/bark"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/wyrd"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

var (
	ErrUnexpectedSelectorOperator  = fmt.Errorf("unexpected requirements operator")
	ErrNoRequirementsValueProvided = fmt.Errorf("no value for a requirement is provided")
)

type DbStore struct {
	db *gorm.DB
}

func NewDbStore(db *gorm.DB) urth.Store {
	return &DbStore{
		db: db,
	}
}

func (s *DbStore) Create(ctx context.Context, value any) error {
	return s.db.WithContext(ctx).Create(value).Error
}

func (s *DbStore) Get(ctx context.Context, dest any, id wyrd.ResourceID) (bool, error) {
	tx := s.db.WithContext(ctx).First(dest, id)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DbStore) GetWithVersion(ctx context.Context, dest any, id wyrd.VersionedResourceId) (bool, error) {
	tx := s.db.WithContext(ctx).Where("version = ?", id.Version).First(dest, id)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DbStore) GetByToken(ctx context.Context, dest any, token urth.ApiToken) (bool, error) {
	tx := s.db.WithContext(ctx).Where("id_token = ?", token).First(dest)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DbStore) Update(ctx context.Context, value any, id wyrd.VersionedResourceId) (bool, error) {
	tx := s.db.WithContext(ctx).Where("version = ?", id.Version).Save(value)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DbStore) Delete(ctx context.Context, value any, id wyrd.VersionedResourceId) (bool, error) {
	tx := s.db.WithContext(ctx).Where("version = ?", id.Version).Delete(value, id.ID)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DbStore) startPaginatedTx(ctx context.Context, pagination bark.Pagination, maxLimit uint) *gorm.DB {
	pagination = pagination.ClampLimit(maxLimit)
	return s.db.WithContext(ctx).Offset(int(pagination.Offset)).Limit(int(pagination.Limit))
}

func (s *DbStore) FindResources(ctx context.Context, resources any, searchQuery bark.SearchQuery, maxLimit uint) (uint, error) {
	selector, err := labels.Parse(searchQuery.Filter)
	if err != nil {
		return 0, fmt.Errorf("error parsing labels selector: %w", err)
	}

	tx, err := s.withSelector(s.startPaginatedTx(ctx, searchQuery.Pagination, maxLimit), selector)
	if err != nil {
		return 0, err
	}

	rtx := tx.Order("created_at").Find(resources)
	if rtx.Error != nil {
		return 0, rtx.Error
	}

	return uint(rtx.RowsAffected), nil
}

func (s *DbStore) withSelector(tx *gorm.DB, selector labels.Selector) (*gorm.DB, error) {
	reqs, ok := selector.Requirements()
	if !ok || len(reqs) == 0 { // Selector has no requirements, easy way out
		return tx, nil
	}

	qs := make([]any, 0, len(reqs))
	for _, req := range reqs {
		switch req.Operator() {
		case selection.Equals, selection.DoubleEquals:
			value, ok := req.Values().PopAny()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			qs = append(qs, JSONQuery("labels").Equals(value, req.Key()))
		case selection.NotEquals:
			value, ok := req.Values().PopAny()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			// not-equals means it exists but value not equal
			qs = append(qs,
				JSONQuery("labels").HasKey(req.Key()),
				JSONQuery("labels").NotEquals(value, req.Key()),
			)
		case selection.GreaterThan:
			value, ok := req.Values().PopAny()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			qs = append(qs, JSONQuery("labels").GreaterThan(value, req.Key()))
		case selection.LessThan:
			value, ok := req.Values().PopAny()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			qs = append(qs, JSONQuery("labels").LessThan(value, req.Key()))

		case selection.In:
			qs = append(qs, JSONQuery("labels").KeyIn(req.Key(), req.Values().UnsortedList()...))
		case selection.NotIn:
			qs = append(qs, JSONQuery("labels").KeyNotIn(req.Key(), req.Values().UnsortedList()...))
		case selection.Exists:
			qs = append(qs, JSONQuery("labels").HasKey(req.Key()))
		case selection.DoesNotExist:
			qs = append(qs, JSONQuery("labels").HasNoKey(req.Key()))
		default:
			return tx, fmt.Errorf("%w: `%v`", ErrUnexpectedSelectorOperator, req.Operator())
		}
	}

	// log.Print("[DEBUG] SQL: ", tx.ToSQL(func(tx *gorm.DB) *gorm.DB {
	// 	for _, c := range qs {
	// 		tx = tx.Where(c)
	// 	}
	// 	return tx.Find(&[]Scenario{})
	// }))

	for _, c := range qs {
		tx = tx.Where(c)
	}

	return tx, nil
}
