// Copyright 2022 Molecula Corp. All rights reserved.

package planner

import (
	"context"
	"fmt"
	"strings"

	pilosa "github.com/featurebasedb/featurebase/v3"
	"github.com/featurebasedb/featurebase/v3/pql"
	"github.com/featurebasedb/featurebase/v3/sql3"
	"github.com/featurebasedb/featurebase/v3/sql3/parser"
	"github.com/featurebasedb/featurebase/v3/sql3/planner/types"
	"github.com/pkg/errors"
)

// PlanOpPQLTableScan plan operator handles a PQL table scan
type PlanOpPQLTableScan struct {
	planner   *ExecutionPlanner
	tableName string
	columns   []string
	filter    types.PlanExpression
	topExpr   types.PlanExpression
	warnings  []string
}

func NewPlanOpPQLTableScan(p *ExecutionPlanner, tableName string, columns []string) *PlanOpPQLTableScan {
	return &PlanOpPQLTableScan{
		planner:   p,
		tableName: tableName,
		columns:   columns,
		warnings:  make([]string, 0),
	}
}

func (p *PlanOpPQLTableScan) Plan() map[string]interface{} {
	result := make(map[string]interface{})
	result["_op"] = fmt.Sprintf("%T", p)
	sc := make([]string, 0)
	for _, e := range p.Schema() {
		sc = append(sc, fmt.Sprintf("'%s', '%s', '%s'", e.ColumnName, e.RelationName, e.Type.TypeName()))
	}
	result["_schema"] = sc

	result["tableName"] = p.tableName

	if p.topExpr != nil {
		result["topExpr"] = p.topExpr.Plan()
	}
	if p.filter != nil {
		result["filter"] = p.filter.Plan()
	}

	result["columns"] = p.columns
	return result
}

func (p *PlanOpPQLTableScan) String() string {
	return ""
}

func (p *PlanOpPQLTableScan) AddWarning(warning string) {
	p.warnings = append(p.warnings, warning)
}

func (p *PlanOpPQLTableScan) Warnings() []string {
	return p.warnings
}

func (p *PlanOpPQLTableScan) Name() string {
	return p.tableName
}

func (p *PlanOpPQLTableScan) UpdateFilters(filterCondition types.PlanExpression) (types.PlanOperator, error) {
	p.filter = filterCondition
	return p, nil
}

func (p *PlanOpPQLTableScan) Schema() types.Schema {
	result := make(types.Schema, 0)

	table, err := p.planner.schemaAPI.IndexInfo(context.Background(), p.tableName)
	if err != nil {
		return result
	}

	for _, col := range table.Fields {
		result = append(result, &types.PlannerColumn{
			ColumnName:   col.Name,
			RelationName: p.tableName,
			Type:         fieldSQLDataType(col),
		})
	}
	return result
}

func (p *PlanOpPQLTableScan) Children() []types.PlanOperator {
	return []types.PlanOperator{}
}

func (p *PlanOpPQLTableScan) Iterator(ctx context.Context, row types.Row) (types.RowIterator, error) {
	return &tableScanRowIter{
		planner:   p.planner,
		tableName: p.tableName,
		columns:   p.columns,
		predicate: p.filter,
		topExpr:   p.topExpr,
	}, nil
}

func (p *PlanOpPQLTableScan) WithChildren(children ...types.PlanOperator) (types.PlanOperator, error) {
	return nil, nil
}

type targetColumn struct {
	columnIdx    int
	srcColumnIdx int
	columnName   string
	dataType     parser.ExprDataType
}

type tableScanRowIter struct {
	planner   *ExecutionPlanner
	tableName string
	columns   []string
	predicate types.PlanExpression
	topExpr   types.PlanExpression

	result    []pilosa.ExtractedTableColumn
	rowWidth  int
	columnMap map[string]*targetColumn
}

var _ types.RowIterator = (*tableScanRowIter)(nil)

func (i *tableScanRowIter) Next(ctx context.Context) (types.Row, error) {
	if i.result == nil {
		err := i.planner.checkAccess(ctx, i.tableName, accessTypeReadData)
		if err != nil {
			return nil, err
		}

		//go get the schema def and map names to indexes in the resultant row
		table, err := i.planner.schemaAPI.IndexInfo(context.Background(), i.tableName)
		if err != nil {
			if errors.Is(err, pilosa.ErrIndexNotFound) {
				return nil, sql3.NewErrInternalf("table not found '%s'", i.tableName)
			}
			return nil, err
		}
		i.rowWidth = len(table.Fields)

		i.columnMap = make(map[string]*targetColumn)
		for idx, fld := range table.Fields {
			i.columnMap[fld.Name] = &targetColumn{
				columnIdx:    idx,
				srcColumnIdx: -1,
				columnName:   fld.Name,
				dataType:     fieldSQLDataType(fld),
			}
		}

		var cond *pql.Call

		cond, err = i.planner.generatePQLCallFromExpr(ctx, i.predicate)
		if err != nil {
			return nil, err
		}
		if cond == nil {
			cond = &pql.Call{Name: "All"}
		}

		if i.topExpr != nil {
			_, ok := i.topExpr.(*intLiteralPlanExpression)
			if !ok {
				return nil, sql3.NewErrInternalf("unexpected top expression type: %T", i.topExpr)
			}
			pqlValue, err := planExprToValue(i.topExpr)
			if err != nil {
				return nil, err
			}
			cond = &pql.Call{
				Name:     "Limit",
				Children: []*pql.Call{cond},
				Args:     map[string]interface{}{"limit": pqlValue},
				Type:     pql.PrecallGlobal,
			}
		}

		call := &pql.Call{Name: "Extract", Children: []*pql.Call{cond}}
		for _, c := range i.columns {

			// skip the _id field
			if strings.EqualFold(c, "_id") {
				continue
			}

			call.Children = append(call.Children,
				&pql.Call{
					Name: "Rows",
					Args: map[string]interface{}{"field": c},
				},
			)
		}
		queryResponse, err := i.planner.executor.Execute(ctx, i.tableName, &pql.Query{Calls: []*pql.Call{call}}, nil, nil)
		if err != nil {
			return nil, err
		}
		tbl, ok := queryResponse.Results[0].(pilosa.ExtractedTable)
		if !ok {
			return nil, sql3.NewErrInternalf("unexpected Extract() result type: %T", queryResponse.Results[0])
		}
		i.result = tbl.Columns

		//set the source index
		for idx, fld := range tbl.Fields {
			mappedColumn, ok := i.columnMap[fld.Name]
			if !ok {
				return nil, sql3.NewErrInternalf("mapped column not found for column named '%s'", fld.Name)
			}
			mappedColumn.srcColumnIdx = idx
		}
	}

	if len(i.result) > 0 {
		row := make([]interface{}, i.rowWidth)

		for _, c := range i.columns {
			result := i.result[0]

			mappedColumn, ok := i.columnMap[c]
			if !ok {
				return nil, sql3.NewErrInternalf("mapped column not found for column named '%s'", c)
			}
			mappedColIdx := mappedColumn.columnIdx
			mappedSrcColIdx := mappedColumn.srcColumnIdx

			if strings.EqualFold(c, "_id") {
				if result.Column.Keyed {
					row[mappedColIdx] = result.Column.Key
				} else {
					row[mappedColIdx] = int64(result.Column.ID)
				}
			} else {
				switch mappedColumn.dataType.(type) {
				case *parser.DataTypeIDSet:
					//empty sets are null
					val, ok := result.Rows[mappedSrcColIdx].([]uint64)
					if !ok {
						return nil, sql3.NewErrInternalf("unexpected type for column value '%T'", result.Rows[mappedSrcColIdx])
					}
					if len(val) == 0 {
						row[mappedColIdx] = nil
					} else {
						row[mappedColIdx] = val
					}

				case *parser.DataTypeStringSet:
					//empty sets are null
					val, ok := result.Rows[mappedSrcColIdx].([]string)
					if !ok {
						return nil, sql3.NewErrInternalf("unexpected type for column value '%T'", result.Rows[mappedSrcColIdx])
					}
					if len(val) == 0 {
						row[mappedColIdx] = nil
					} else {
						row[mappedColIdx] = val
					}

				default:
					row[mappedColIdx] = result.Rows[mappedSrcColIdx]
				}
			}
		}

		// Move to next result element.
		i.result = i.result[1:]
		return row, nil
	}
	return nil, types.ErrNoMoreRows
}
