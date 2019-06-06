package dtestutils

import (
	"github.com/attic-labs/noms/go/types"
	"github.com/google/uuid"
	"github.com/liquidata-inc/ld/dolt/go/libraries/doltcore/row"
	"github.com/liquidata-inc/ld/dolt/go/libraries/doltcore/schema"
	"github.com/liquidata-inc/ld/dolt/go/libraries/doltcore/table"
	"github.com/liquidata-inc/ld/dolt/go/libraries/doltcore/table/untyped"
	"strconv"
)

var UUIDS = []uuid.UUID{
	uuid.Must(uuid.Parse("00000000-0000-0000-0000-000000000000")),
	uuid.Must(uuid.Parse("00000000-0000-0000-0000-000000000001")),
	uuid.Must(uuid.Parse("00000000-0000-0000-0000-000000000002"))}
var Names = []string{"Bill Billerson", "John Johnson", "Rob Robertson"}
var Ages = []uint64{32, 25, 21}
var Titles = []string{"Senior Dufus", "Dufus", ""}
var MaritalStatus = []bool{true, false, false}

const (
	IdTag uint64 = iota
	NameTag
	AgeTag
	IsMarriedTag
	TitleTag
	NextTag // leave last
)

var typedColColl, _ = schema.NewColCollection(
	schema.NewColumn("id", IdTag, types.UUIDKind, true, schema.NotNullConstraint{}),
	schema.NewColumn("name", NameTag, types.StringKind, false, schema.NotNullConstraint{}),
	schema.NewColumn("age", AgeTag, types.UintKind, false, schema.NotNullConstraint{}),
	schema.NewColumn("is_married", IsMarriedTag, types.BoolKind, false, schema.NotNullConstraint{}),
	schema.NewColumn("title", TitleTag, types.StringKind, false),
)

var TypedSchema = schema.SchemaFromCols(typedColColl)
var UntypedSchema = untyped.UntypeSchema(TypedSchema)
var TypedRows []row.Row
var UntypedRows []row.Row

func init() {
	for i := 0; i < len(UUIDS); i++ {

		taggedVals := row.TaggedValues{
			IdTag:        types.UUID(UUIDS[i]),
			NameTag:      types.String(Names[i]),
			AgeTag:       types.Uint(Ages[i]),
			TitleTag:     types.String(Titles[i]),
			IsMarriedTag: types.Bool(MaritalStatus[i]),
		}

		marriedStr := "true"
		if !MaritalStatus[i] {
			marriedStr = "false"
		}

		TypedRows = append(TypedRows, row.New(TypedSchema, taggedVals))

		taggedVals = row.TaggedValues{
			IdTag:        types.String(UUIDS[i].String()),
			NameTag:      types.String(Names[i]),
			AgeTag:       types.String(strconv.FormatUint(Ages[i], 10)),
			TitleTag:     types.String(Titles[i]),
			IsMarriedTag: types.String(marriedStr),
		}

		UntypedRows = append(UntypedRows, row.New(UntypedSchema, taggedVals))
	}
}

func CreateTestDataTable(typed bool) (*table.InMemTable, schema.Schema) {
	sch := TypedSchema
	rows := TypedRows
	if !typed {
		sch = UntypedSchema
		rows = UntypedRows
	}

	imt := table.NewInMemTable(sch)

	for _, r := range rows {
		err := imt.AppendRow(r)
		if err != nil {
			panic(err)
		}
	}

	return imt, sch
}
