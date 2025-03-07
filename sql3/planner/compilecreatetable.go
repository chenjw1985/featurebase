// Copyright 2021 Molecula Corp. All rights reserved.

package planner

import (
	"strconv"
	"strings"
	"time"

	pilosa "github.com/featurebasedb/featurebase/v3"
	"github.com/featurebasedb/featurebase/v3/pql"
	"github.com/featurebasedb/featurebase/v3/sql3"
	"github.com/featurebasedb/featurebase/v3/sql3/parser"
	"github.com/featurebasedb/featurebase/v3/sql3/planner/types"
)

type createTableField struct {
	planner  *ExecutionPlanner
	name     string
	typeName string
	fos      []pilosa.FieldOption
}

// compileCreateTableStatement compiles a CREATE TABLE statement into a
// PlanOperator.
func (p *ExecutionPlanner) compileCreateTableStatement(stmt *parser.CreateTableStatement) (_ types.PlanOperator, err error) {
	tableName := parser.IdentName(stmt.Name)
	failIfExists := !stmt.IfNotExists.IsValid()

	// apply table options
	keyPartitions := 0
	for _, option := range stmt.Options {
		switch o := option.(type) {
		case *parser.KeyPartitionsOption:
			e := o.Expr.(*parser.IntegerLit)
			i, err := strconv.ParseInt(e.Value, 10, 64)
			if err != nil {
				return nil, err
			}
			keyPartitions = int(i)
		}
	}

	isKeyed := false

	var columns = []*createTableField{}
	for _, col := range stmt.Columns {
		columnName := parser.IdentName(col.Name)
		typeName := parser.IdentName(col.Type.Name)

		if strings.ToLower(columnName) == "_id" {
			if strings.EqualFold(typeName, parser.FieldTypeString) {
				isKeyed = true
			}
			continue
		}

		column, err := p.compileColumn(col)
		if err != nil {
			return nil, err
		}

		columns = append(columns, column)
	}
	return NewPlanOpQuery(NewPlanOpCreateTable(p, tableName, failIfExists, isKeyed, keyPartitions, columns), p.sql), nil
}

// compiles a column def
func (p *ExecutionPlanner) compileColumn(col *parser.ColumnDefinition) (*createTableField, error) {
	var err error
	columnName := parser.IdentName(col.Name)
	typeName := parser.IdentName(col.Type.Name)

	column := &createTableField{
		planner:  p,
		name:     columnName,
		typeName: typeName,
	}
	// Possible FieldOptions. We define these here, but the contraints below
	// can set them to the values provided in the create table statement.
	// And finally, the correct pilosa.FieldOption functional option will be
	// created after that.
	var cacheType string = pilosa.DefaultCacheType
	var cacheSize uint32 = pilosa.DefaultCacheSize
	var scale int64
	min, max := pql.MinMax(0)
	var epoch = pilosa.DefaultEpoch
	var timeUnit string = pilosa.TimeUnitSeconds
	var timeQuantum pilosa.TimeQuantum
	var ttl = "0"

	for _, con := range col.Constraints {
		switch c := con.(type) {
		case *parser.CacheTypeConstraint:
			cacheType = c.CacheTypeValue

			if c.Size.IsValid() {
				e := c.SizeExpr.(*parser.IntegerLit)
				i, err := strconv.ParseInt(e.Value, 10, 64)
				if err != nil {
					return nil, err
				}
				cacheSize = uint32(i)
			}

		case *parser.MinConstraint:
			var val string
			switch e := c.Expr.(type) {
			case *parser.IntegerLit:
				val = e.Value

			case *parser.UnaryExpr:
				// Call analyzeUnaryExpression() in order to set the value on
				// expr.ResultDataType so that we can rely on the DataType()
				// method. There is a case where a BITNOT expression could get
				// through, but that value as a string will fail in
				// strconv.ParseInt() conversion.
				if _, err := p.analyzeUnaryExpression(e, nil); err != nil {
					return nil, err
				}

				if e.IsLiteral() && typeIsInteger(e.DataType()) {
					val = e.String()
				}
			}

			i, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return nil, err
			}
			min = pql.NewDecimal(i, 0)

		case *parser.MaxConstraint:
			var val string
			switch e := c.Expr.(type) {
			case *parser.IntegerLit:
				val = e.Value

			case *parser.UnaryExpr:
				// Call analyzeUnaryExpression() in order to set the value on
				// expr.ResultDataType so that we can rely on the DataType()
				// method. There is a case where a BITNOT expression could get
				// through, but that value as a string will fail in
				// strconv.ParseInt() conversion.
				if _, err := p.analyzeUnaryExpression(e, nil); err != nil {
					return nil, err
				}

				if e.IsLiteral() && typeIsInteger(e.DataType()) {
					val = e.String()
				}
			}

			i, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return nil, err
			}
			max = pql.NewDecimal(i, 0)

		case *parser.TimeUnitConstraint:
			unit := c.Expr.(*parser.StringLit)
			timeUnit = unit.Value

			epochString := c.EpochExpr.(*parser.StringLit)
			tm, err := time.ParseInLocation(time.RFC3339, epochString.Value, time.UTC)
			if err != nil {
				return nil, sql3.NewErrInvalidTimeEpoch(c.EpochExpr.Pos().Line, c.EpochExpr.Pos().Line, epochString.Value)
			}
			epoch = tm

		case *parser.TimeQuantumConstraint:
			unit := c.Expr.(*parser.StringLit)
			timeQuantum = pilosa.TimeQuantum(unit.Value)
			if c.TtlExpr != nil {
				e := c.TtlExpr.(*parser.StringLit)
				ttl = e.Value
			}

		default:
			return nil, sql3.NewErrInternalf("unhandled column constraint type '%T'", c)
		}
	}

	switch strings.ToUpper(typeName) {
	case parser.FieldTypeBool:
		column.fos = append(column.fos, pilosa.OptFieldTypeBool())
	case parser.FieldTypeDecimal:
		// Get the scale value.
		scale, err = strconv.ParseInt(col.Type.Scale.Value, 10, 64)
		if err != nil {
			return nil, err
		}

		// Adjust min/max to fit within the scaled min/max.
		scaledMin, scaledMax := pql.MinMax(scale)
		if scaledMax.LessThan(max) {
			max = scaledMax
		}
		if scaledMin.GreaterThan(min) {
			min = scaledMin
		}

		column.fos = append(column.fos, pilosa.OptFieldTypeDecimal(scale, min, max))
	case parser.FieldTypeID:
		column.fos = append(column.fos, pilosa.OptFieldTypeMutex(cacheType, cacheSize))
	case parser.FieldTypeIDSet:
		if timeQuantum != "" {
			column.fos = append(column.fos, pilosa.OptFieldTypeTime(timeQuantum, ttl))
		} else {
			column.fos = append(column.fos, pilosa.OptFieldTypeSet(cacheType, cacheSize))
		}
	case parser.FieldTypeInt:
		column.fos = append(column.fos, pilosa.OptFieldTypeInt(min.ToInt64(0), max.ToInt64(0)))
	case parser.FieldTypeString:
		column.fos = append(column.fos, pilosa.OptFieldTypeMutex(cacheType, cacheSize))
		column.fos = append(column.fos, pilosa.OptFieldKeys())
	case parser.FieldTypeStringSet:
		if timeQuantum != "" {
			column.fos = append(column.fos, pilosa.OptFieldTypeTime(timeQuantum, ttl))
		} else {
			column.fos = append(column.fos, pilosa.OptFieldTypeSet(cacheType, cacheSize))
		}
		column.fos = append(column.fos, pilosa.OptFieldKeys())
	case parser.FieldTypeTimestamp:
		column.fos = append(column.fos, pilosa.OptFieldTypeTimestamp(epoch, timeUnit))
	}
	return column, nil
}

// analyzeCreateTableStatement analyzes a CREATE TABLE statement and returns an
// error if anything is invalid.
func (p *ExecutionPlanner) analyzeCreateTableStatement(stmt *parser.CreateTableStatement) error {
	//iterate columns, check types, check constraints, ensure we have no dupe names and make sure there is an _id column
	checkedColumns := make(map[string]string)
	for _, col := range stmt.Columns {
		columnName := parser.IdentName(col.Name)
		_, ok := checkedColumns[strings.ToLower(columnName)]
		if ok {
			return sql3.NewErrDuplicateColumn(col.Name.NamePos.Line, col.Name.NamePos.Column, columnName)
		}

		typeName := parser.IdentName(col.Type.Name)
		if !parser.IsValidTypeName(typeName) {
			return sql3.NewErrUnknownType(col.Type.Name.NamePos.Line, col.Type.Name.NamePos.Column, typeName)
		}

		if strings.ToLower(columnName) == "_id" {
			//check the type
			if !(strings.EqualFold(typeName, parser.FieldTypeID) || strings.EqualFold(typeName, parser.FieldTypeString)) {
				return sql3.NewErrTableIDColumnType(col.Type.Name.NamePos.Line, col.Type.Name.NamePos.Column)
			}
			//make sure we have no constraints
			if len(col.Constraints) > 0 {
				return sql3.NewErrTableIDColumnConstraints(col.Type.Name.NamePos.Line, col.Type.Name.NamePos.Column)
			}
		}
		checkedColumns[columnName] = strings.ToLower(columnName)

		err := p.analyzeColumn(typeName, col)
		if err != nil {
			return err
		}
	}
	_, ok := checkedColumns["_id"]
	if !ok {
		return sql3.NewErrTableMustHaveIDColumn(stmt.Create.Line, stmt.Create.Column)
	}
	//check table options
	for _, option := range stmt.Options {

		switch o := option.(type) {
		case *parser.KeyPartitionsOption:
			//check the type of the expression
			literal, ok := o.Expr.(*parser.IntegerLit)
			if !ok {
				return sql3.NewErrIntegerLiteral(o.Expr.Pos().Line, o.Expr.Pos().Column)
			}
			//key partittions needs to be >=1 and we'll cap conservatively at 10000
			i, err := strconv.ParseInt(literal.Value, 10, 64)
			if err != nil {
				return err
			}
			if i < 1 || i > 10000 {
				return sql3.NewErrInvalidKeyPartitionsValue(o.Expr.Pos().Line, o.Expr.Pos().Column, i)
			}

		case *parser.ShardWidthOption:
			//check the type of the expression
			literal, ok := o.Expr.(*parser.IntegerLit)
			if !ok {
				return sql3.NewErrIntegerLiteral(o.Expr.Pos().Line, o.Expr.Pos().Column)
			}
			//shardwidth needs to be a power of 2 and > 2^16
			i, err := strconv.ParseInt(literal.Value, 10, 64)
			if err != nil {
				return err
			}
			isPwrOf2 := (i & (i - 1)) == 0
			if (i == 0) || !isPwrOf2 || i < (1<<16) {
				return sql3.NewErrInvalidShardWidthValue(o.Expr.Pos().Line, o.Expr.Pos().Column, i)
			}

		default:
			return sql3.NewErrInternalf("unhandled table option type '%T'", option)
		}
	}

	return nil
}

// analyze the column def for a CREATE or ALTER TABLE
func (p *ExecutionPlanner) analyzeColumn(typeName string, col *parser.ColumnDefinition) error {
	// handledConstraints keeps track of the constraints which have been
	// analyzed in the for loop below. This allows us to verify that two
	// different, incompatible constraints aren't included. For now, that
	// really only applies to TIMEQUANTUM and CACHETYPE. The other
	// constraints which may be incompatible are checked against the field
	// type. It may make sense, in the future, to add some logic which
	// analyzes all the constraints in a more flexible way, but this
	// addresses the immediate issue.
	handledConstraints := make(map[parser.Token]struct{})

	for _, con := range col.Constraints {
		switch c := con.(type) {
		case *parser.CacheTypeConstraint:
			//make sure we have a set or mutex type
			if !(strings.EqualFold(typeName, parser.FieldTypeString) || strings.EqualFold(typeName, parser.FieldTypeStringSet) || strings.EqualFold(typeName, parser.FieldTypeID) || strings.EqualFold(typeName, parser.FieldTypeIDSet)) {
				return sql3.NewErrBadColumnConstraint(col.Name.NamePos.Line, col.Name.NamePos.Column, "CACHETYPE", typeName)
			}
			//check the type of the expression for SIZE
			if c.Size.IsValid() {
				if _, ok := c.SizeExpr.(*parser.IntegerLit); !ok {
					return sql3.NewErrIntegerLiteral(c.SizeExpr.Pos().Line, c.SizeExpr.Pos().Column)
				}
			}
			// Make sure a TIMEQUANTUM constraint (which is incompatible with
			// CACHETYPE) hasn't been specified.
			if _, ok := handledConstraints[parser.TIMEQUANTUM]; ok {
				return sql3.NewErrConflictingColumnConstraint(col.Name.NamePos.Line, col.Name.NamePos.Column, parser.CACHETYPE, parser.TIMEQUANTUM)
			}
			handledConstraints[parser.CACHETYPE] = struct{}{}

		case *parser.MinConstraint:
			// Make sure we have either an integer or unary type.
			switch c.Expr.(type) {
			case *parser.IntegerLit, *parser.UnaryExpr:
				// pass
			default:
				return sql3.NewErrBadColumnConstraint(col.Name.NamePos.Line, col.Name.NamePos.Column, "MIN", typeName)
			}
			handledConstraints[parser.MIN] = struct{}{}

		case *parser.MaxConstraint:
			// Make sure we have either an integer or unary type.
			switch c.Expr.(type) {
			case *parser.IntegerLit, *parser.UnaryExpr:
				// pass
			default:
				return sql3.NewErrBadColumnConstraint(col.Name.NamePos.Line, col.Name.NamePos.Column, "MAX", typeName)
			}
			handledConstraints[parser.MAX] = struct{}{}

		case *parser.TimeUnitConstraint:
			//make sure we have an timestamp type
			if !strings.EqualFold(typeName, parser.FieldTypeTimestamp) {
				return sql3.NewErrBadColumnConstraint(col.Name.NamePos.Line, col.Name.NamePos.Column, "TIMEUNIT", typeName)
			}
			//check the type of the expression
			unit, ok := c.Expr.(*parser.StringLit)
			if !ok {
				return sql3.NewErrStringLiteral(c.Expr.Pos().Line, c.Expr.Pos().Column)
			}
			if !pilosa.IsValidTimeUnit(unit.Value) {
				return sql3.NewErrInvalidTimeUnit(c.Expr.Pos().Line, c.Expr.Pos().Column, unit.Value)
			}
			if c.EpochExpr != nil {
				//check the type of the expression
				_, ok := c.EpochExpr.(*parser.StringLit)
				if !ok {
					return sql3.NewErrStringLiteral(c.EpochExpr.Pos().Line, c.EpochExpr.Pos().Column)
				}
			}
			handledConstraints[parser.TIMEUNIT] = struct{}{}

		case *parser.TimeQuantumConstraint:
			//make sure we have a set type
			if !(strings.EqualFold(typeName, parser.FieldTypeStringSet) || strings.EqualFold(typeName, parser.FieldTypeIDSet)) {
				return sql3.NewErrBadColumnConstraint(col.Name.NamePos.Line, col.Name.NamePos.Column, "TIMEQUANTUM", typeName)
			}
			//check the type of the expression
			unit, ok := c.Expr.(*parser.StringLit)
			if !ok {
				return sql3.NewErrStringLiteral(c.Expr.Pos().Line, c.Expr.Pos().Column)
			}
			quantum := pilosa.TimeQuantum(strings.ToUpper(unit.Value))
			if !quantum.Valid() {
				return sql3.NewErrInvalidTimeQuantum(c.Expr.Pos().Line, c.Expr.Pos().Column, unit.Value)
			}
			if c.TtlExpr != nil {
				//check the type of the expression
				ttl, ok := c.TtlExpr.(*parser.StringLit)
				if !ok {
					return sql3.NewErrStringLiteral(c.Expr.Pos().Line, c.Expr.Pos().Column)
				}
				_, err := time.ParseDuration(ttl.Value)
				if err != nil {
					return sql3.NewErrInvalidDuration(c.Expr.Pos().Line, c.Expr.Pos().Column, ttl.Value)
				}
			}

			// Make sure a CACHETYPE constraint (which is incompatible with
			// TIMEQUANTUM) hasn't been specified.
			if _, ok := handledConstraints[parser.CACHETYPE]; ok {
				return sql3.NewErrConflictingColumnConstraint(col.Name.NamePos.Line, col.Name.NamePos.Column, parser.CACHETYPE, parser.TIMEQUANTUM)
			}

			handledConstraints[parser.TIMEQUANTUM] = struct{}{}

		default:
			return sql3.NewErrInternalf("unhandled column constraint type '%T'", c)
		}
	}
	return nil
}
