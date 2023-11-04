package urth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"

	"github.com/sre-norns/urth/pkg/wyrd"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func NewDbStore(db *gorm.DB) Store {
	return &DbStore{
		db: db,
	}
}

func (s *DbStore) Create(ctx context.Context, value any) error {
	return s.db.WithContext(ctx).Create(value).Error
}

func (s *DbStore) Get(ctx context.Context, dest any, id ResourceID) (bool, error) {
	tx := s.db.WithContext(ctx).First(dest, id)

	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DbStore) GetWithVersion(ctx context.Context, dest any, id VersionedResourceId) (bool, error) {
	tx := s.db.WithContext(ctx).Where("version = ?", id.Version).First(dest, id)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DbStore) Update(ctx context.Context, value any, id ResourceID) (bool, error) {
	tx := s.db.WithContext(ctx).Save(value)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DbStore) Delete(ctx context.Context, value any, id ResourceID) (bool, error) {
	tx := s.db.WithContext(ctx).Delete(value, id)
	return tx.RowsAffected == 1, tx.Error
}

func (s *DbStore) GuessKind(value any) (TypeMeta, error) {
	stmt := &gorm.Statement{DB: s.db}
	if err := stmt.Parse(value); err != nil {
		return TypeMeta{}, err
	}

	return TypeMeta{Kind: stmt.Schema.Table}, nil
}

func (s *DbStore) startPaginatedTx(ctx context.Context, pagination Pagination) *gorm.DB {
	return s.db.WithContext(ctx).Offset(int(pagination.Offset)).Limit(int(pagination.Limit))
}

func (s *DbStore) FindResources(ctx context.Context, resources any, searchQuery SearchQuery) (TypeMeta, error) {
	var resultType TypeMeta

	selector, err := labels.Parse(searchQuery.Labels)
	if err != nil {
		return resultType, err
	}

	tx, err := s.selectorAsQuery(s.startPaginatedTx(ctx, searchQuery.Pagination).Preload(clause.Associations), selector)
	if err != nil {
		return resultType, err
	}

	rtx := tx.Find(resources)
	if rtx.Error != nil {
		return resultType, rtx.Error
	}

	t := reflect.ValueOf(resources) // TypeOf(resources)
	if t.Kind() == reflect.Slice && t.Len() > 0 {
		resultType, err = s.GuessKind(t.Index(0))
		if err != nil {
			return resultType, err
		}
	}

	return resultType, nil
}

func (s *DbStore) FindInto(ctx context.Context, model any, into any, pagination Pagination) error {
	return s.startPaginatedTx(ctx, pagination).Model(model).Group("key").Group("value").Find(into).Error
}

func (s *DbStore) selectorAsQuery(tx *gorm.DB, selector labels.Selector) (*gorm.DB, error) {
	reqs, ok := selector.Requirements()
	if !ok || len(reqs) == 0 { // Selector has no requirements, easy way out
		return tx, nil
	}

	// subQuery := tx.Session(&gorm.Session{NewDB: true}).Model(&ResourceLabelModel{}).Distinct("owner_id")
	subQuery := s.db.Model(&ResourceLabelModel{}).Distinct("owner_id")
	for _, req := range reqs {
		switch req.Operator() {
		case selection.Equals, selection.DoubleEquals:
			value, ok := req.Values().PopAny()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			subQuery = subQuery.Where("key = ? AND value = ?", req.Key(), value)
		case selection.NotEquals:
			value, ok := req.Values().PopAny()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			subQuery = subQuery.Where("key = ? AND value <> ?", req.Key(), value)
		case selection.GreaterThan:
			value, ok := req.Values().PopAny()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			subQuery = subQuery.Where("key = ? AND value > ?", req.Key(), value)
		case selection.LessThan:
			value, ok := req.Values().PopAny()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			subQuery = subQuery.Where("key = ? AND value < ?", req.Key(), value)

		case selection.In:
			subQuery = subQuery.Where("key = ? AND value IN ?", req.Key(), req.Values().UnsortedList())
		case selection.NotIn:
			subQuery = subQuery.Where("key = ? AND value NOT IN ?", req.Key(), req.Values().UnsortedList())
		case selection.Exists:
			subQuery = subQuery.Where("key = ?", req.Key())
		case selection.DoesNotExist:
			subSubQuery := tx.Session(&gorm.Session{NewDB: true}).Model(&ResourceLabelModel{}).Distinct("owner_id").Where("key = ?", req.Key())
			subQuery = subQuery.Where("owner_id NOT IN (?)", subSubQuery)
		default:
			return nil, fmt.Errorf("%w: `%v`", ErrUnexpectedSelectorOperator, req.Operator())
		}
	}

	log.Printf("SQL: %v", tx.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&Scenario{}).Where("id in (?)", subQuery).Find(&[]Scenario{})
	}))

	return tx.Where("id in (?)", subQuery), nil
}

func (meta *ResourceMeta) AfterFind(tx *gorm.DB) (err error) {
	if len(meta.Labels) != len(meta.LabelsModel) {
		meta.Labels = make(wyrd.Labels, len(meta.LabelsModel))
		for _, label := range meta.LabelsModel {
			meta.Labels[label.Key] = label.Value
		}
	}

	return
}
