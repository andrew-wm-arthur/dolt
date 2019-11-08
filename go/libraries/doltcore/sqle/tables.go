// Copyright 2019 Liquidata, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sqle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/src-d/go-mysql-server/sql"

	"github.com/liquidata-inc/dolt/go/cmd/dolt/errhand"
	"github.com/liquidata-inc/dolt/go/libraries/doltcore/doltdb"
	"github.com/liquidata-inc/dolt/go/libraries/doltcore/schema"
	"github.com/liquidata-inc/dolt/go/store/types"
)

var _ sql.Table = (*DoltTable)(nil)
var _ sql.UpdatableTable = (*DoltTable)(nil)
var _ sql.DeletableTable = (*DoltTable)(nil)
var _ sql.InsertableTable = (*DoltTable)(nil)

// DoltTable implements the sql.Table interface and gives access to dolt table rows and schema.
type DoltTable struct {
	name  string
	table *doltdb.Table
	sch   schema.Schema
	db    *Database
}

// Implements sql.IndexableTable
func (t *DoltTable) WithIndexLookup(lookup sql.IndexLookup) sql.Table {
	dil, ok := lookup.(*doltIndexLookup)
	if !ok {
		panic(fmt.Sprintf("Unrecognized indexLookup %T", lookup))
	}

	return &IndexedDoltTable{
		table:       t,
		indexLookup: dil,
	}
}

// Implements sql.IndexableTable
func (t *DoltTable) IndexKeyValues(*sql.Context, []string) (sql.PartitionIndexKeyValueIter, error) {
	return nil, errors.New("creating new indexes not supported")
}

// Implements sql.IndexableTable
func (t *DoltTable) IndexLookup() sql.IndexLookup {
	panic("IndexLookup called on DoltTable, should be on IndexedDoltTable")
}

// Name returns the name of the table.
func (t *DoltTable) Name() string {
	return t.name
}

// Not sure what the purpose of this method is, so returning the name for now.
func (t *DoltTable) String() string {
	return t.name
}

// Schema returns the schema for this table.
func (t *DoltTable) Schema() sql.Schema {
	// TODO: fix panics
	sch, err := t.table.GetSchema(context.TODO())

	if err != nil {
		panic(err)
	}

	// TODO: fix panics
	sqlSch, err := doltSchemaToSqlSchema(t.name, sch)

	if err != nil {
		panic(err)
	}

	return sqlSch
}

// Returns the partitions for this table. We return a single partition, but could potentially get more performance by
// returning multiple.
func (t *DoltTable) Partitions(*sql.Context) (sql.PartitionIter, error) {
	return &doltTablePartitionIter{}, nil
}

// Returns the table rows for the partition given (all rows of the table).
func (t *DoltTable) PartitionRows(ctx *sql.Context, _ sql.Partition) (sql.RowIter, error) {
	return newRowIterator(t, ctx)
}

type inserter struct {
	t  *DoltTable
	ed *types.MapEditor
}

var _ sql.RowInserter = (*inserter)(nil)

func (i *inserter) init(ctx context.Context) error {
	if i.ed != nil {
		return nil
	}

	var err error
	i.ed, err = i.t.newMapEditor(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (d *DoltTable) newMapEditor(ctx context.Context) (*types.MapEditor, error) {
	typesMap, err := d.table.GetRowData(ctx)
	if err != nil {
		return nil, errhand.BuildDError("failed to get row data.").AddCause(err).Build()
	}

	return typesMap.Edit(), nil
}

func (i *inserter) Insert(ctx *sql.Context, sqlRow sql.Row) error {
	dRow, err := SqlRowToDoltRow(i.t.table.Format(), sqlRow, i.t.sch)
	if err != nil {
		return err
	}

	key, err := dRow.NomsMapKey(i.t.sch).Value(ctx)
	if err != nil {
		return errhand.BuildDError("failed to get row key").AddCause(err).Build()
	}
	_, rowExists, err := i.t.table.GetRow(ctx, key.(types.Tuple), i.t.sch)
	if err != nil {
		return errhand.BuildDError("failed to read table").AddCause(err).Build()
	}
	if rowExists {
		return errors.New("duplicate primary key given")
	}

	if err = i.init(ctx); err != nil {
		return err
	}
	i.ed = i.ed.Set(key, dRow.NomsMapValue(i.t.sch))

	return nil
}

func (i *inserter) Close(ctx *sql.Context) error {
	return i.t.updateTable(ctx, i.ed)
}

// Inserter returns a RowInserter for this table
func (t *DoltTable) Inserter(ctx *sql.Context) sql.RowInserter {
	return &inserter{
		t:  t,
	}
}

type deleter struct {
	t  *DoltTable
	ed *types.MapEditor
}

var _ sql.RowDeleter = (*deleter)(nil)

func (t *DoltTable) Deleter(*sql.Context, sql.Row) sql.RowDeleter {
	return &deleter{
		t:  t,
	}
}

func (d *deleter) Delete(ctx *sql.Context, sqlRow sql.Row) error {
	dRow, err := SqlRowToDoltRow(d.t.table.Format(), sqlRow, d.t.sch)
	if err != nil {
		return err
	}

	key, err := dRow.NomsMapKey(d.t.sch).Value(ctx)
	if err != nil {
		return errhand.BuildDError("failed to get row key").AddCause(err).Build()
	}

	if d.ed == nil {
		d.ed, err = d.t.newMapEditor(ctx)
		if err != nil {
			return err
		}
	}

	d.ed = d.ed.Remove(key)
	return nil
}

func (d *deleter) Close(ctx *sql.Context) error {
	return d.t.updateTable(ctx, d.ed)
}

type updater struct {
	t  *DoltTable
	ed *types.MapEditor
}

var _ sql.RowUpdater = (*updater)(nil)

func (t *DoltTable) Updater(ctx *sql.Context) sql.RowUpdater {
	return &updater{
		t:  t,
	}
}

func (u *updater) Update(ctx *sql.Context, oldRow sql.Row, newRow sql.Row) error {
	dOldRow, err := SqlRowToDoltRow(u.t.table.Format(), oldRow, u.t.sch)
	if err != nil {
		return err
	}
	dNewRow, err := SqlRowToDoltRow(u.t.table.Format(), newRow, u.t.sch)
	if err != nil {
		return err
	}

	// If the PK is changed then we have to delete the old row first
	// This is assuming that the new PK is not taken
	dOldKey := dOldRow.NomsMapKey(u.t.sch)
	dOldKeyVal, err := dOldKey.Value(ctx)
	if err != nil {
		return err
	}
	dNewKey := dNewRow.NomsMapKey(u.t.sch)
	dNewKeyVal, err := dNewKey.Value(ctx)
	if err != nil {
		return err
	}

	if u.ed == nil {
		u.ed, err = u.t.newMapEditor(ctx)
		if err != nil {
			return err
		}
	}

	if !dOldKeyVal.Equals(dNewKeyVal) {
		_, rowExists, err := u.t.table.GetRow(ctx, dNewKeyVal.(types.Tuple), u.t.sch)
		if err != nil {
			return errhand.BuildDError("failed to read table").AddCause(err).Build()
		}
		if rowExists {
			newRowAsStrings := make([]string, len(newRow))
			for i, val := range newRow {
				newRowAsStrings[i] = fmt.Sprintf("%v", val)
			}
			return fmt.Errorf("primary key collision: (%v)", strings.Join(newRowAsStrings, ", "))
		}
		u.ed.Remove(dOldKey)
	} else {
		u.ed.Set(dOldKey, dNewRow.NomsMapValue(u.t.sch))
	}

	return nil
}

func (u *updater) Close(ctx *sql.Context) error {
	return u.t.updateTable(ctx, u.ed)
}

// doltTablePartitionIter, an object that knows how to return the single partition exactly once.
type doltTablePartitionIter struct {
	sql.PartitionIter
	i int
}

// Close is required by the sql.PartitionIter interface. Does nothing.
func (itr *doltTablePartitionIter) Close() error {
	return nil
}

// Next returns the next partition if there is one, or io.EOF if there isn't.
func (itr *doltTablePartitionIter) Next() (sql.Partition, error) {
	if itr.i > 0 {
		return nil, io.EOF
	}
	itr.i++

	return &doltTablePartition{}, nil
}

// A table partition, currently an unused layer of abstraction but required for the framework.
type doltTablePartition struct {
	sql.Partition
}

const partitionName = "single"

// Key returns the key for this partition, which must uniquely identity the partition. We have only a single partition
// per table, so we use a constant.
func (p doltTablePartition) Key() []byte {
	return []byte(partitionName)
}

func (t *DoltTable) updateTable(ctx *sql.Context, mapEditor *types.MapEditor) error {
	updated, err := mapEditor.Map(ctx)
	if err != nil {
		return errhand.BuildDError("failed to modify table").AddCause(err).Build()
	}

	newTable, err := t.table.UpdateRows(ctx, updated)
	if err != nil {
		return errhand.BuildDError("failed to update rows").AddCause(err).Build()
	}

	newRoot, err := doltdb.PutTable(ctx, t.db.root, t.db.root.VRW(), t.name, newTable)
	if err != nil {
		return errhand.BuildDError("failed to write table back to database").AddCause(err).Build()
	}

	t.table = newTable
	t.db.root = newRoot
	return nil
}
