/*
 * Copyright (C) 2025 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not
 * use this file except in compliance with the License. You may obtain a copy of
 * the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
 * WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
 * License for the specific language governing permissions and limitations under
 * the License.
 */

package translator

import (
	"reflect"
	"testing"

	schemaMapping "github.com/GoogleCloudPlatform/cloud-bigtable-ecosystem/cassandra-bigtable-migration-tools/cassandra-bigtable-proxy/schema-mapping"
	cql "github.com/GoogleCloudPlatform/cloud-bigtable-ecosystem/cassandra-bigtable-migration-tools/cassandra-bigtable-proxy/third_party/cqlparser"
	"github.com/antlr4-go/antlr/v4"
	"github.com/datastax/go-cassandra-native-protocol/datatype"
	"github.com/datastax/go-cassandra-native-protocol/message"
	"github.com/datastax/go-cassandra-native-protocol/primitive"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func parseInsertQuery(queryString string) cql.IInsertContext {
	query := renameLiterals(queryString)
	lexer := cql.NewCqlLexer(antlr.NewInputStream(query))
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	p := cql.NewCqlParser(stream)
	insertObj := p.Insert()
	insertObj.KwInto()
	return insertObj
}

func Test_setParamsFromValues(t *testing.T) {
	var protocalV primitive.ProtocolVersion = 4
	response := make(map[string]interface{})
	val, _ := formatValues("Test", "text", protocalV)
	specialCharVal, _ := formatValues("#!@#$%^&*()_+", "text", protocalV)
	response["name"] = val
	specialCharResponse := make(map[string]interface{})
	specialCharResponse["name"] = specialCharVal
	var respValue []interface{}
	var emptyRespValue []interface{}
	respValue = append(respValue, val)
	specialCharRespValue := []interface{}{specialCharVal}
	unencrypted := make(map[string]interface{})
	unencrypted["name"] = "Test"
	unencryptedForSpecialChar := make(map[string]interface{})
	unencryptedForSpecialChar["name"] = "#!@#$%^&*()_+"
	type args struct {
		input           cql.IInsertValuesSpecContext
		columns         []Column
		schemaMapping   *schemaMapping.SchemaMappingConfig
		protocolV       primitive.ProtocolVersion
		isPreparedQuery bool
	}
	tests := []struct {
		name    string
		args    args
		want    map[string]interface{}
		want1   []interface{}
		want2   map[string]interface{}
		wantErr bool
	}{
		{
			name: "success with special characters",
			args: args{
				input: parseInsertQuery("INSERT INTO xobani_derived.user_info ( name ) VALUES ('#!@#$%^&*()_+')").InsertValuesSpec(),
				columns: []Column{
					{
						Name:         "name",
						ColumnFamily: "cf1",
						CQLType:      "text",
					},
				},
				schemaMapping:   GetSchemaMappingConfig(),
				protocolV:       protocalV,
				isPreparedQuery: false,
			},
			want:    specialCharResponse,
			want1:   specialCharRespValue,
			want2:   unencryptedForSpecialChar,
			wantErr: false,
		},
		{
			name: "success",
			args: args{
				input: parseInsertQuery("INSERT INTO xobani_derived.user_info ( name ) VALUES ('Test')").InsertValuesSpec(),
				columns: []Column{
					{
						Name:         "name",
						ColumnFamily: "cf1",
						CQLType:      "text",
					},
				},
				schemaMapping:   GetSchemaMappingConfig(),
				protocolV:       protocalV,
				isPreparedQuery: false,
			},
			want:    response,
			want1:   respValue,
			want2:   unencrypted,
			wantErr: false,
		},
		{
			name: "success in prepare query",
			args: args{
				input: parseInsertQuery("INSERT INTO xobani_derived.user_info ( name ) VALUES ('?')").InsertValuesSpec(),
				columns: []Column{
					{
						Name:         "name",
						ColumnFamily: "cf1",
						CQLType:      "text",
					},
				},
				schemaMapping:   GetSchemaMappingConfig(),
				protocolV:       protocalV,
				isPreparedQuery: true,
			},
			want:    make(map[string]interface{}),
			want1:   emptyRespValue,
			want2:   make(map[string]interface{}),
			wantErr: false,
		},
		{
			name: "failed",
			args: args{
				input: nil,
				columns: []Column{
					{
						Name:         "name",
						ColumnFamily: "cf1",
						CQLType:      "text",
					},
				},
				schemaMapping:   GetSchemaMappingConfig(),
				protocolV:       protocalV,
				isPreparedQuery: false,
			},
			want:    nil,
			want1:   nil,
			want2:   nil,
			wantErr: true,
		},
		{
			name: "failed",
			args: args{
				input: parseInsertQuery("INSERT INTO xobani_derived.user_info ( name ) VALUES").InsertValuesSpec(),
				columns: []Column{
					{
						Name:         "name",
						ColumnFamily: "cf1",
						CQLType:      "text",
					},
				},
				schemaMapping:   GetSchemaMappingConfig(),
				protocolV:       protocalV,
				isPreparedQuery: false,
			},
			want:    nil,
			want1:   nil,
			want2:   nil,
			wantErr: true,
		},
		{
			name: "failed",
			args: args{
				input:           parseInsertQuery("INSERT INTO xobani_derived.user_info ( name ) VALUES").InsertValuesSpec(),
				columns:         nil,
				schemaMapping:   GetSchemaMappingConfig(),
				protocolV:       protocalV,
				isPreparedQuery: false,
			},
			want:    nil,
			want1:   nil,
			want2:   nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, got2, err := setParamsFromValues(tt.args.input, tt.args.columns, tt.args.schemaMapping, tt.args.protocolV, "user_info", "test_keyspace", tt.args.isPreparedQuery)
			if (err != nil) != tt.wantErr {
				t.Errorf("setParamsFromValues() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("setParamsFromValues() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("setParamsFromValues() got1 = %v, want %v", got1, tt.want1)
			}
			if !reflect.DeepEqual(got2, tt.want2) {
				t.Errorf("setParamsFromValues() got2 = %v, want %v", got2, tt.want2)
			}
		})
	}
}

func TestTranslator_TranslateInsertQuerytoBigtable(t *testing.T) {
	var protocolV primitive.ProtocolVersion = 4
	type fields struct {
		Logger              *zap.Logger
		SchemaMappingConfig *schemaMapping.SchemaMappingConfig
	}
	type args struct {
		queryStr        string
		protocolV       primitive.ProtocolVersion
		isPreparedQuery bool
	}

	// Define values and format them
	textValue := "test-text"
	blobValue := "0x0000000000000003"
	booleanValue := "true"
	timestampValue := "2024-08-12T12:34:56Z"
	intValue := "123"
	bigIntValue := "1234567890"
	column10 := "column10"

	formattedText, _ := formatValues(textValue, "text", protocolV)
	formattedBlob, _ := formatValues(blobValue, "blob", protocolV)
	formattedBoolean, _ := formatValues(booleanValue, "boolean", protocolV)
	formattedTimestamp, _ := formatValues(timestampValue, "timestamp", protocolV)
	formattedInt, _ := formatValues(intValue, "int", protocolV)
	formattedBigInt, _ := formatValues(bigIntValue, "bigint", protocolV)
	formattedcolumn10text, _ := formatValues(column10, "text", protocolV)

	values := []interface{}{
		formattedBlob,
		formattedBoolean,
		formattedTimestamp,
		formattedInt,
		formattedBigInt,
	}

	response := map[string]interface{}{
		"column1":  formattedText,
		"column2":  formattedBlob,
		"column3":  formattedBoolean,
		"column5":  formattedTimestamp,
		"column6":  formattedInt,
		"column9":  formattedBigInt,
		"column10": formattedcolumn10text,
	}

	query := "INSERT INTO test_keyspace.test_table (column1, column2, column3, column5, column6, column9, column10) VALUES ('" +
		textValue + "', '" + blobValue + "', " + booleanValue + ", '" + timestampValue + "', " + intValue + ", " + bigIntValue + ", " + column10 + ")"

	preparedQuery := "INSERT INTO test_keyspace.test_table (column1, column2, column3, column5, column6, column9, column10) VALUES ('?', '?', '?', '?', '?', '?', '?')"
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *InsertQueryMapping
		wantErr bool
	}{
		{
			name: "success with prepared query",
			args: args{
				queryStr:        preparedQuery,
				protocolV:       protocolV,
				isPreparedQuery: true,
			},
			fields: fields{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			want: &InsertQueryMapping{
				Query:     preparedQuery,
				QueryType: "INSERT",
				Table:     "test_table",
				Keyspace:  "test_keyspace",
				Columns: []Column{
					{Name: "column1", ColumnFamily: "cf1", CQLType: "varchar", IsPrimaryKey: true},
					{Name: "column2", ColumnFamily: "cf1", CQLType: "blob", IsPrimaryKey: false},
					{Name: "column3", ColumnFamily: "cf1", CQLType: "boolean", IsPrimaryKey: false},
					{Name: "column5", ColumnFamily: "cf1", CQLType: "timestamp", IsPrimaryKey: false},
					{Name: "column6", ColumnFamily: "cf1", CQLType: "int", IsPrimaryKey: false},
					{Name: "column9", ColumnFamily: "cf1", CQLType: "bigint", IsPrimaryKey: false},
					{Name: "column10", ColumnFamily: "cf1", CQLType: "varchar", IsPrimaryKey: true},
				},
				Values:      values,
				Params:      response,
				ParamKeys:   []string{"column1", "column2", "column3", "column5", "column6", "column9", "column10"},
				PrimaryKeys: []string{"column1", "column10"}, // assuming column1 and column10 are primary keys
				RowKey:      "test-text\x00\x01column10",     // assuming row key format
			},
			wantErr: false,
		},
		{
			name: "success",
			args: args{
				queryStr:        query,
				protocolV:       protocolV,
				isPreparedQuery: false,
			},
			fields: fields{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			want: &InsertQueryMapping{
				Query:     query,
				QueryType: "INSERT",
				Table:     "test_table",
				Keyspace:  "test_keyspace",
				Columns: []Column{
					{Name: "column2", ColumnFamily: "cf1", CQLType: "blob", IsPrimaryKey: false},
					{Name: "column3", ColumnFamily: "cf1", CQLType: "boolean", IsPrimaryKey: false},
					{Name: "column5", ColumnFamily: "cf1", CQLType: "timestamp", IsPrimaryKey: false},
					{Name: "column6", ColumnFamily: "cf1", CQLType: "int", IsPrimaryKey: false},
					{Name: "column9", ColumnFamily: "cf1", CQLType: "bigint", IsPrimaryKey: false},
				},
				Values:      values,
				Params:      response,
				ParamKeys:   []string{"column1", "column2", "column3", "column5", "column6", "column9", "column10"},
				PrimaryKeys: []string{"column1", "column10"}, // assuming column1 and column10 are primary keys
				RowKey:      "test-text\x00\x01column10",     // assuming row key format
			},
			wantErr: false,
		},
		/*{
			name: "failed scenario 1",
			args: args{
				queryStr:        "INSERT INTO test_keyspace.test_table;",
				protocolV:       protocolV,
				isPreparedQuery: false,
			},
			fields: fields{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "failed scenario 2",
			args: args{
				queryStr:  "INSERT INTO test_keyspace.test_table (column1) VALUES ",
				protocolV: protocolV,
			},
			fields: fields{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "failed scenario 3",
			args: args{
				queryStr:        "INSERT INTO test_keyspace.test_table VALUES ('test')",
				protocolV:       protocolV,
				isPreparedQuery: false,
			},
			fields: fields{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "failed because one primary column is not present",
			args: args{
				queryStr: "INSERT INTO test_keyspace.test_table (column1, column2, column3, column5, column6, column9) VALUES ('" +
					textValue + "', '" + blobValue + "', " + booleanValue + ", '" + timestampValue + "', " + intValue + ", " + bigIntValue + ")",
				protocolV:       protocolV,
				isPreparedQuery: false,
			},
			fields: fields{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			want:    nil,
			wantErr: true,
		},
		*/
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &Translator{
				Logger:                       zap.NewNop(),
				EncodeIntValuesWithBigEndian: false,
				SchemaMappingConfig:          tt.fields.SchemaMappingConfig,
			}
			got, err := tr.TranslateInsertQuerytoBigtable(tt.args.queryStr, tt.args.protocolV, tt.args.isPreparedQuery)
			if (err != nil) != tt.wantErr {
				t.Errorf("Translator.TranslateInsertQuerytoBigtable() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != nil && len(got.Params) > 0 {
				assert.Equal(t, tt.want.Params, got.Params)
			}

			if got != nil && len(got.ParamKeys) > 0 {
				assert.Equal(t, tt.want.ParamKeys, got.ParamKeys)
			}
			if got != nil && len(got.Values) > 0 {
				assert.Equal(t, tt.want.Values, got.Values)
			}
			if got != nil && len(got.Columns) > 0 {
				assert.Equal(t, tt.want.Columns, got.Columns)
			}
			if got != nil && len(got.RowKey) > 0 {
				assert.Equal(t, tt.want.RowKey, got.RowKey)
			}

			if got != nil {
				assert.Equal(t, tt.want.Keyspace, got.Keyspace)
			}
		})
	}
}

func TestTranslator_BuildInsertPrepareQuery(t *testing.T) {
	type fields struct {
		Logger              *zap.Logger
		SchemaMappingConfig *schemaMapping.SchemaMappingConfig
	}
	type args struct {
		columnsResponse []Column
		values          []*primitive.Value
		st              *InsertQueryMapping
		protocolV       primitive.ProtocolVersion
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *InsertQueryMapping
		wantErr bool
	}{
		{
			name: "Valid Input",
			fields: fields{
				Logger:              zap.NewNop(),
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			args: args{
				columnsResponse: []Column{
					{
						Name:         "pk_1_text",
						ColumnFamily: "cf1",
						CQLType:      "text",
						IsPrimaryKey: true,
					},
				},
				values: []*primitive.Value{
					{Contents: []byte("")},
				},
				st: &InsertQueryMapping{
					Query:       "INSERT INTO test_keyspace.test_table(pk_1_text) VALUES (?)",
					QueryType:   "Insert",
					Table:       "test_table",
					Keyspace:    "test_keyspace",
					PrimaryKeys: []string{"pk_1_text"},
					RowKey:      "",
					Columns: []Column{
						{
							Name:         "pk_1_text",
							ColumnFamily: "cf1",
							CQLType:      "text",
							IsPrimaryKey: true,
						},
					},
					Params: map[string]interface{}{
						"pk_1_text": &primitive.Value{Contents: []byte("123")},
					},
					ParamKeys:     []string{"pk_1_text"},
					IfNotExists:   false,
					TimestampInfo: TimestampInfo{},
					VariableMetadata: []*message.ColumnMetadata{
						{
							Name: "pk_1_text",
							Type: datatype.Varchar,
						},
					},
				},
				protocolV: 4,
			},
			want: &InsertQueryMapping{
				Query:       "INSERT INTO test_keyspace.test_table(pk_1_text) VALUES (?)",
				QueryType:   "Insert",
				Table:       "test_table",
				Keyspace:    "test_keyspace",
				PrimaryKeys: []string{"pk_1_text"},
				RowKey:      "",
				Columns: []Column{
					{
						Name:         "pk_1_text",
						ColumnFamily: "cf1",
						CQLType:      "text",
						IsPrimaryKey: true,
					},
				},
				Values: []interface{}{
					&primitive.Value{Contents: []byte("123")},
				},
				Params: map[string]interface{}{
					"pk_1_text": &primitive.Value{Contents: []byte("123")},
				},
				ParamKeys:     []string{"pk_1_text"},
				IfNotExists:   false,
				TimestampInfo: TimestampInfo{},
				VariableMetadata: []*message.ColumnMetadata{
					{
						Name: "pk_1_text",
						Type: datatype.Varchar,
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &Translator{
				Logger:              tt.fields.Logger,
				SchemaMappingConfig: tt.fields.SchemaMappingConfig,
			}
			got, err := tr.BuildInsertPrepareQuery(tt.args.columnsResponse, tt.args.values, tt.args.st, tt.args.protocolV)
			if (err != nil) != tt.wantErr {
				t.Errorf("Translator.BuildInsertPrepareQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got.RowKey != tt.want.RowKey {
				t.Errorf("Translator.BuildInsertPrepareQuery() RowKey = %v, want %v", got.RowKey, tt.want.RowKey)
			}
			if len(got.Values) != len(tt.want.Values) {
				t.Errorf("Translator.BuildInsertPrepareQuery() Values length mismatch: got %d, want %d", len(got.Values), len(tt.want.Values))
				return
			}
		})
	}
}
