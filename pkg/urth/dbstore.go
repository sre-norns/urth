package urth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"

	"github.com/sre-norns/urth/pkg/wyrd"
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

func (s *DbStore) GetByToken(ctx context.Context, dest any, token ApiToken) (bool, error) {
	tx := s.db.WithContext(ctx).Where("id_token = ?", token).First(dest)
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

func (s *DbStore) GuessKind(value reflect.Value) (TypeMeta, error) {
	kind, err := guessDbTable(s.db, value.Interface())
	// stmt := &gorm.Statement{DB: s.db}
	// if err := stmt.Parse(value.Interface()); err != nil {
	// 	return TypeMeta{}, err
	// }

	return TypeMeta{Kind: kind}, err
}

func (s *DbStore) startPaginatedTx(ctx context.Context, pagination Pagination) *gorm.DB {
	return s.db.WithContext(ctx).Offset(int(pagination.Offset)).Limit(int(pagination.Limit))
}

func (s *DbStore) FindResourcesWithEx(ctx context.Context, owner_id ResourceID, resources any, searchQuery SearchQuery) (TypeMeta, error) {
	var resultType TypeMeta

	selector, err := labels.Parse(searchQuery.Labels)
	if err != nil {
		return resultType, err
	}

	tx := s.startPaginatedTx(ctx, searchQuery.Pagination).Where("scenario_id = ?", owner_id)
	cond, err := s.selectorAsQuery(selector, tx)
	if err != nil {
		return resultType, err
	}

	rtx := tx.Find(resources, cond)
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

func (s *DbStore) FindResources(ctx context.Context, resources any, searchQuery SearchQuery) (TypeMeta, error) {
	var resultType TypeMeta

	selector, err := labels.Parse(searchQuery.Labels)
	if err != nil {
		return resultType, err
	}

	tx := s.startPaginatedTx(ctx, searchQuery.Pagination)
	cond, err := s.selectorAsQuery(selector, tx)
	if err != nil {
		return resultType, err
	}

	for _, c := range cond {
		tx = tx.Where(c)
	}

	rtx := tx.Find(resources)
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

func (s *DbStore) FindInto(ctx context.Context, model any, into any, pagination Pagination) error {
	return s.startPaginatedTx(ctx, pagination).Model(model).Group("key").Group("value").Find(into).Error
}

func (s *DbStore) selectorAsQuery(selector labels.Selector, tx *gorm.DB) ([]any, error) {
	reqs, ok := selector.Requirements()
	if !ok || len(reqs) == 0 { // Selector has no requirements, easy way out
		return nil, nil
	}

	qs := make([]any, 0, len(reqs))
	for _, req := range reqs {
		switch req.Operator() {
		case selection.Equals, selection.DoubleEquals:
			value, ok := req.Values().PopAny()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			qs = append(qs, JSONQuery("attributes").Equals(value, req.Key()))
		case selection.NotEquals:
			value, ok := req.Values().PopAny()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			qs = append(qs, JSONQuery("attributes").NotEquals(value, req.Key()))
		case selection.GreaterThan:
			value, ok := req.Values().PopAny()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			qs = append(qs, JSONQuery("attributes").GreaterThan(value, req.Key()))
		case selection.LessThan:
			value, ok := req.Values().PopAny()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			qs = append(qs, JSONQuery("attributes").LessThan(value, req.Key()))

		case selection.In:
			qs = append(qs, JSONQuery("attributes").KeyIn(req.Key(), req.Values().UnsortedList()...))
		case selection.NotIn:
			qs = append(qs, JSONQuery("attributes").KeyNotIn(req.Key(), req.Values().UnsortedList()...))
		case selection.Exists:
			qs = append(qs, JSONQuery("attributes").HasKey(req.Key()))
		case selection.DoesNotExist:
			qs = append(qs, JSONQuery("attributes").HasNoKey(req.Key()))
		default:
			return nil, fmt.Errorf("%w: `%v`", ErrUnexpectedSelectorOperator, req.Operator())
		}
	}

	log.Print("[DEBUG] SQL: ", tx.ToSQL(func(tx *gorm.DB) *gorm.DB {
		for _, c := range qs {
			tx = tx.Where(c)
		}
		return tx.Find(&[]Scenario{})
	}))

	return qs, nil
}

func (meta *ResourceMeta) AfterFind(tx *gorm.DB) (err error) {
	if meta.Attributes == nil {
		meta.Labels = wyrd.Labels{}
		return nil
	}

	return json.Unmarshal(meta.Attributes, &meta.Labels)
}

func (meta *ResourceMeta) BeforeSave(tx *gorm.DB) (err error) {
	meta.Attributes, err = json.Marshal(meta.Labels)
	return
}
