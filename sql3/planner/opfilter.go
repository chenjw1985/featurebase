// Copyright 2022 Molecula Corp. All rights reserved.

package planner

import (
	"context"
	"fmt"

	"github.com/featurebasedb/featurebase/v3/sql3"
	"github.com/featurebasedb/featurebase/v3/sql3/planner/types"
)

// PlanOpFilter is a filter operator
type PlanOpFilter struct {
	planner   *ExecutionPlanner
	ChildOp   types.PlanOperator
	Predicate types.PlanExpression

	warnings []string
}

func NewPlanOpFilter(planner *ExecutionPlanner, predicate types.PlanExpression, child types.PlanOperator) *PlanOpFilter {
	return &PlanOpFilter{
		planner:   planner,
		Predicate: predicate,
		ChildOp:   child,
		warnings:  make([]string, 0),
	}
}

func (p *PlanOpFilter) Schema() types.Schema {
	return p.ChildOp.Schema()
}

func (p *PlanOpFilter) Iterator(ctx context.Context, row types.Row) (types.RowIterator, error) {
	i, err := p.ChildOp.Iterator(ctx, row)
	if err != nil {
		return nil, err
	}
	return newFilterIterator(ctx, p.Predicate, i), nil
}

func (p *PlanOpFilter) WithChildren(children ...types.PlanOperator) (types.PlanOperator, error) {
	if len(children) != 1 {
		return nil, sql3.NewErrInternalf("unexpected number of children '%d'", len(children))
	}
	return NewPlanOpFilter(p.planner, p.Predicate, children[0]), nil
}

func (p *PlanOpFilter) Children() []types.PlanOperator {
	return []types.PlanOperator{
		p.ChildOp,
	}
}

func (p *PlanOpFilter) Plan() map[string]interface{} {
	result := make(map[string]interface{})
	result["_op"] = fmt.Sprintf("%T", p)
	ps := make([]string, 0)
	for _, e := range p.Schema() {
		ps = append(ps, fmt.Sprintf("'%s', '%s', '%s'", e.ColumnName, e.RelationName, e.Type.TypeName()))
	}
	result["_schema"] = ps
	result["child"] = p.ChildOp.Plan()
	return result
}

func (p *PlanOpFilter) String() string {
	return ""
}

func (p *PlanOpFilter) AddWarning(warning string) {
	p.warnings = append(p.warnings, warning)
}

func (p *PlanOpFilter) Warnings() []string {
	return p.warnings
}

type filterIterator struct {
	predicate types.PlanExpression
	child     types.RowIterator
	ctx       context.Context
}

func newFilterIterator(ctx context.Context, predicate types.PlanExpression, child types.RowIterator) *filterIterator {
	return &filterIterator{
		ctx:       ctx,
		predicate: predicate,
		child:     child,
	}
}

func (i *filterIterator) Next(ctx context.Context) (types.Row, error) {
	//TODO (pok) - actually implement the filter
	return i.child.Next(ctx)
}
