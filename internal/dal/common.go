package dal

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("dal: not found")

type Condition struct {
	Query string
	Args  []any
}

type OrderParam struct {
	Column string
	Desc   bool
}

type QueryParam struct {
	Where  []Condition
	Orders []OrderParam
	Limit  int
}

type UpdateParam struct {
	Where          []Condition
	Values         map[string]any
	TouchUpdatedAt bool
}

func Eq(column string, value any) Condition {
	return Condition{
		Query: fmt.Sprintf("%s = ?", column),
		Args:  []any{value},
	}
}

func ApplyQuery(ctx context.Context, db *gorm.DB, param QueryParam) *gorm.DB {
	tx := db.WithContext(ctx)
	for _, condition := range param.Where {
		tx = tx.Where(condition.Query, condition.Args...)
	}
	for _, order := range param.Orders {
		if order.Column == "" {
			continue
		}
		direction := "ASC"
		if order.Desc {
			direction = "DESC"
		}
		tx = tx.Order(fmt.Sprintf("%s %s", order.Column, direction))
	}
	if param.Limit > 0 {
		tx = tx.Limit(param.Limit)
	}
	return tx
}

func ApplyUpdate(ctx context.Context, db *gorm.DB, param UpdateParam) *gorm.DB {
	tx := db.WithContext(ctx)
	for _, condition := range param.Where {
		tx = tx.Where(condition.Query, condition.Args...)
	}
	values := make(map[string]any, len(param.Values)+1)
	for key, value := range param.Values {
		values[key] = value
	}
	if param.TouchUpdatedAt {
		values["updated_at"] = gorm.Expr("CURRENT_TIMESTAMP")
	}
	return tx.Updates(values)
}

func OrderBy(column string, desc bool) OrderParam {
	return OrderParam{Column: column, Desc: desc}
}

func Ping(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}
