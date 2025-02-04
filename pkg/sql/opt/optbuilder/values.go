// Copyright 2018 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package optbuilder

import (
	"fmt"

	"github.com/cockroachdb/cockroach/pkg/sql/opt/memo"
	"github.com/cockroachdb/cockroach/pkg/sql/pgwire/pgcode"
	"github.com/cockroachdb/cockroach/pkg/sql/pgwire/pgerror"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
	"github.com/cockroachdb/cockroach/pkg/sql/types"
)

// buildValuesClause builds a set of memo groups that represent the given values
// clause.
//
// See Builder.buildStmt for a description of the remaining input and
// return values.
func (b *Builder) buildValuesClause(
	values *tree.ValuesClause, desiredTypes []*types.T, inScope *scope,
) (outScope *scope) {
	numRows := len(values.Rows)
	numCols := 0
	if numRows > 0 {
		numCols = len(values.Rows[0])
	}

	// We need to save and restore the previous value of the field in
	// semaCtx in case we are recursively called within a subquery
	// context.
	defer b.semaCtx.Properties.Restore(b.semaCtx.Properties)

	// Ensure there are no special functions in the clause.
	b.semaCtx.Properties.Require(exprKindValues.String(), tree.RejectSpecial)
	inScope.context = exprKindValues

	// Typing a VALUES clause is not trivial; consider:
	//   VALUES (NULL), (1)
	// We want to type the entire column as INT. For this, we must find the first
	// expression that resolves to a definite type. Moreover, we want the NULL to
	// be typed correctly; so once we figure out the type, we must go back and
	// add casts as necessary. We do this column by column and store the groups in
	// a linearized matrix.

	// Bulk allocate ScalarListExpr slices: we need a matrix of size numRows by
	// numCols and one slice of length numRows for the tuples.
	elems := make(memo.ScalarListExpr, numRows*numCols+numRows)
	tuples, elems := elems[:numRows], elems[numRows:]

	colTypes := make([]*types.T, numCols)
	for colIdx := range colTypes {
		desired := types.Any
		if colIdx < len(desiredTypes) {
			desired = desiredTypes[colIdx]
		}
		colTypes[colIdx] = types.Unknown

		elemPos := colIdx
		for _, tuple := range values.Rows {
			if numCols != len(tuple) {
				reportValuesLenError(numCols, len(tuple))
			}

			expr := inScope.walkExprTree(tuple[colIdx])
			texpr, err := tree.TypeCheck(b.ctx, expr, b.semaCtx, desired)
			if err != nil {
				panic(err)
			}
			if typ := texpr.ResolvedType(); typ.Family() != types.UnknownFamily {
				if colTypes[colIdx].Family() == types.UnknownFamily {
					colTypes[colIdx] = typ
				} else if rightHasMoreSpecificTuple(colTypes[colIdx], typ) {
					// This condition handles the case when an earlier expression in the
					// VALUES clause is an array of AnyTuple, but a later expression is an
					// array of a more specific tuple type.
					colTypes[colIdx] = typ
				} else if !typ.Equivalent(colTypes[colIdx]) {
					panic(pgerror.Newf(pgcode.DatatypeMismatch,
						"VALUES types %s and %s cannot be matched", typ, colTypes[colIdx]))
				}
			}
			elems[elemPos] = b.buildScalar(texpr, inScope, nil, nil, nil)
			elemPos += numCols
		}

		// If we still don't have a type for the column, set it to the desired type.
		if colTypes[colIdx].Family() == types.UnknownFamily && desired.Family() != types.AnyFamily {
			colTypes[colIdx] = desired
		}

		// Add casts to NULL values if necessary.
		if colTypes[colIdx].Family() != types.UnknownFamily {
			elemPos := colIdx
			for range values.Rows {
				if elems[elemPos].DataType().Family() == types.UnknownFamily {
					elems[elemPos] = b.factory.ConstructCast(elems[elemPos], colTypes[colIdx])
				}
				elemPos += numCols
			}
		}
	}

	// Build the tuples.
	tupleTyp := types.MakeTuple(colTypes)
	for rowIdx := range tuples {
		tuples[rowIdx] = b.factory.ConstructTuple(elems[:numCols], tupleTyp)
		elems = elems[numCols:]
	}

	outScope = inScope.push()
	for colIdx := 0; colIdx < numCols; colIdx++ {
		// The column names for VALUES are column1, column2, etc.
		colName := scopeColName(tree.Name(fmt.Sprintf("column%d", colIdx+1)))
		b.synthesizeColumn(outScope, colName, colTypes[colIdx], nil, nil /* scalar */)
	}

	colList := colsToColList(outScope.cols)
	outScope.expr = b.factory.ConstructValues(tuples, &memo.ValuesPrivate{
		Cols: colList,
		ID:   b.factory.Metadata().NextUniqueID(),
	})
	return outScope
}

func reportValuesLenError(expected, actual int) {
	panic(pgerror.Newf(
		pgcode.Syntax,
		"VALUES lists must all be the same length, expected %d columns, found %d",
		expected, actual))
}

// rightHasMoreSpecificTuple returns true if the left and right types are
// equivalent, but the right type is constrained by a more specific nested
// tuple than the left type.
func rightHasMoreSpecificTuple(left, right *types.T) bool {
	if left.Family() != right.Family() {
		return false
	}
	if left.Family() == types.ArrayFamily && right.Family() == types.ArrayFamily {
		return rightHasMoreSpecificTuple(left.ArrayContents(), right.ArrayContents())
	}
	if left.Family() == types.TupleFamily && right.Family() == types.TupleFamily {
		if right == types.AnyTuple {
			return false
		} else if left == types.AnyTuple {
			return true
		} else if len(left.TupleContents()) != len(right.TupleContents()) {
			return false
		}
		ret := true
		for i, leftElem := range left.TupleContents() {
			rightElem := right.TupleContents()[i]
			// Only recurse if we are dealing with a container type.
			if leftElem.Family() == types.ArrayFamily || leftElem.Family() == types.TupleFamily {
				ret = ret && rightHasMoreSpecificTuple(leftElem, rightElem)
			} else {
				ret = ret && leftElem.Equivalent(rightElem)
			}
		}
		return ret
	}
	// For non-tuples and non-arrays, we don't want the right type to get
	// preference over the left type.
	return false
}
