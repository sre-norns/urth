package dbstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/sre-norns/urth/pkg/urth"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

var (
	ErrUnexpectedSelectorOperator  = fmt.Errorf("unexpected requirements operator")
	ErrNoRequirementsValueProvided = fmt.Errorf("no value for a requirement is provided")
)

func guessDbTable(db *gorm.DB, value any) (string, error) {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(value); err != nil {
		return "", err
	}

	return stmt.Schema.Table, nil
}

type DbStore struct {
	db *gorm.DB
}

func NewDbStore(db *gorm.DB) urth.Store {
	return &DbStore{
		db: db,
	}
}

func (s *DbStore) Create(ctx context.Context, value any) (urth.TypeMeta, error) {
	kind, err := s.GuessKind(reflect.ValueOf(value))
	if err != nil {
		return kind, err
	}

	return kind, s.db.WithContext(ctx).Create(value).Error
}

func (s *DbStore) Get(ctx context.Context, dest any, id urth.ResourceID) (bool, error) {
	tx := s.db.WithContext(ctx).First(dest, id)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DbStore) GetWithVersion(ctx context.Context, dest any, id urth.VersionedResourceId) (bool, error) {
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

func (s *DbStore) Update(ctx context.Context, value any, id urth.VersionedResourceId) (bool, error) {
	tx := s.db.WithContext(ctx).Where("version = ?", id.Version).Save(value)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DbStore) Delete(ctx context.Context, value any, id urth.VersionedResourceId) (bool, error) {
	tx := s.db.WithContext(ctx).Where("version = ?", id.Version).Delete(value, id.ID)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DbStore) GuessKind(value reflect.Value) (urth.TypeMeta, error) {
	kind, err := guessDbTable(s.db, value.Interface())

	return urth.TypeMeta{Kind: kind}, err
}

func (s *DbStore) startPaginatedTx(ctx context.Context, pagination urth.Pagination) *gorm.DB {
	return s.db.WithContext(ctx).Offset(int(pagination.Offset)).Limit(int(pagination.Limit))
}

func (s *DbStore) FindResources(ctx context.Context, resources any, searchQuery urth.SearchQuery) (urth.TypeMeta, error) {
	var resultType urth.TypeMeta

	selector, err := labels.Parse(searchQuery.Labels)
	if err != nil {
		return resultType, fmt.Errorf("error parsing labels selector: %w", err)
	}

	tx, err := s.withSelector(s.startPaginatedTx(ctx, searchQuery.Pagination), selector)
	if err != nil {
		return resultType, err
	}

	rtx := tx.Order("created_at").Find(resources)
	if rtx.Error != nil {
		return resultType, rtx.Error
	}

	t := reflect.ValueOf(resources).Elem()
	if (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) && t.Len() > 0 {
		resultType, err = s.GuessKind(reflect.Zero(t.Type()))
		if err != nil {
			return resultType, err
		}
	}

	return resultType, nil
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
