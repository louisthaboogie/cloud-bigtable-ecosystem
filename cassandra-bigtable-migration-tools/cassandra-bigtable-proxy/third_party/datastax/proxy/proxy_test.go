// Copyright (c) DataStax, Inc.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//      http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package proxy

import (
	"bytes"
	"context"
	"crypto/md5"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/bigtable"
	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	bigtableModule "github.com/GoogleCloudPlatform/cloud-bigtable-ecosystem/cassandra-bigtable-migration-tools/cassandra-bigtable-proxy/bigtable"
	rh "github.com/GoogleCloudPlatform/cloud-bigtable-ecosystem/cassandra-bigtable-migration-tools/cassandra-bigtable-proxy/responsehandler"
	"github.com/GoogleCloudPlatform/cloud-bigtable-ecosystem/cassandra-bigtable-migration-tools/cassandra-bigtable-proxy/third_party/datastax/parser"
	lru "github.com/hashicorp/golang-lru"
	"google.golang.org/grpc"

	bt "github.com/GoogleCloudPlatform/cloud-bigtable-ecosystem/cassandra-bigtable-migration-tools/cassandra-bigtable-proxy/bigtable"
	otelgo "github.com/GoogleCloudPlatform/cloud-bigtable-ecosystem/cassandra-bigtable-migration-tools/cassandra-bigtable-proxy/otel"
	"github.com/GoogleCloudPlatform/cloud-bigtable-ecosystem/cassandra-bigtable-migration-tools/cassandra-bigtable-proxy/third_party/datastax/proxycore"
	"github.com/datastax/go-cassandra-native-protocol/datatype"
	"github.com/datastax/go-cassandra-native-protocol/frame"
	"github.com/datastax/go-cassandra-native-protocol/message"
	"github.com/datastax/go-cassandra-native-protocol/primitive"

	mockbigtable "github.com/GoogleCloudPlatform/cloud-bigtable-ecosystem/cassandra-bigtable-migration-tools/cassandra-bigtable-proxy/mocks/bigtable"
	"github.com/GoogleCloudPlatform/cloud-bigtable-ecosystem/cassandra-bigtable-migration-tools/cassandra-bigtable-proxy/responsehandler"
	schemaMapping "github.com/GoogleCloudPlatform/cloud-bigtable-ecosystem/cassandra-bigtable-migration-tools/cassandra-bigtable-proxy/schema-mapping"
	"github.com/GoogleCloudPlatform/cloud-bigtable-ecosystem/cassandra-bigtable-migration-tools/cassandra-bigtable-proxy/translator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"go.uber.org/zap"
)

func TestConnect(t *testing.T) {
	var logger *zap.Logger
	logger = proxycore.GetOrCreateNopLogger(logger)
	mp := make(map[*client]struct{})
	prox := &Proxy{
		clients: mp,
		logger:  logger,
	}
	err := prox.Connect()
	assert.NoError(t, err, "function should return no error")
}

func TestNameBasedUUID(t *testing.T) {
	uuid := nameBasedUUID("test")
	assert.NotNilf(t, uuid, "should not be nil")
}

func Test_addclient(t *testing.T) {
	relativePath := "fakedata/service_account.json"

	// Get the absolute path by resolving the relative path
	absolutePath, err := filepath.Abs(relativePath)
	if err != nil {
		assert.NoErrorf(t, err, "should not through an error")
	}
	newPath := strings.Replace(absolutePath, "/proxy/", "/", 1)
	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", newPath)

	mp := make(map[*client]struct{})
	prox := &Proxy{
		clients: mp,
	}

	assert.NoErrorf(t, err, "no error expected")
	cl := &client{
		ctx:                 prox.ctx,
		proxy:               prox,
		preparedSystemQuery: make(map[[preparedIdSize]byte]interface{}),
		preparedQuerys:      make(map[[preparedIdSize]byte]interface{}),
	}
	prox.addClient(cl)
	prox.registerForEvents(cl)
}

func Test_OnEvent(t *testing.T) {
	var logger *zap.Logger
	relativePath := "fakedata/service_account.json"

	// Get the absolute path by resolving the relative path
	absolutePath, err := filepath.Abs(relativePath)
	if err != nil {
		assert.NoErrorf(t, err, "should not through an error")
	}
	newPath := strings.Replace(absolutePath, "/proxy/", "/", 1)
	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", newPath)

	logger = proxycore.GetOrCreateNopLogger(logger)
	mp := make(map[*client]struct{})
	prox := &Proxy{
		clients: mp,
		logger:  logger,
	}
	assert.NoErrorf(t, err, "no error expected")
	prox.OnEvent(&proxycore.SchemaChangeEvent{Message: &message.SchemaChangeEvent{ChangeType: primitive.SchemaChangeType(primitive.EventTypeSchemaChange)}})

}

func Test_Serve(t *testing.T) {
	var logger *zap.Logger
	logger = proxycore.GetOrCreateNopLogger(logger)
	relativePath := "fakedata/service_account.json"

	// Get the absolute path by resolving the relative path
	absolutePath, err := filepath.Abs(relativePath)
	if err != nil {
		assert.NoErrorf(t, err, "should not through an error")
	}
	newPath := strings.Replace(absolutePath, "/proxy/", "/", 1)
	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", newPath)

	logger = proxycore.GetOrCreateNopLogger(logger)
	mp := make(map[*client]struct{})
	ls := make(map[*net.Listener]struct{})
	prox := &Proxy{
		clients:   mp,
		logger:    logger,
		listeners: ls,
	}
	assert.NoErrorf(t, err, "no error expected")

	var httpListener net.Listener
	httpListener, err = resolveAndListen(":7777", false, "", "", "", logger)
	assert.NoErrorf(t, err, "no error expected")

	prox.isConnected = true
	go func() {
		time.Sleep(5 * time.Second)
		prox.isClosing = true
		httpListener.Close()
	}()
	err = prox.Serve(httpListener)
	assert.Errorf(t, err, "error expected")

}

func Test_ServeScenario2(t *testing.T) {
	var logger *zap.Logger
	relativePath := "fakedata/service_account.json"

	// Get the absolute path by resolving the relative path
	absolutePath, err := filepath.Abs(relativePath)
	if err != nil {
		assert.NoErrorf(t, err, "should not through an error")
	}
	newPath := strings.Replace(absolutePath, "/proxy/", "/", 1)
	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", newPath)

	logger = proxycore.GetOrCreateNopLogger(logger)
	mp := make(map[*client]struct{})
	ls := make(map[*net.Listener]struct{})
	cl := make(chan struct{})
	prox := &Proxy{
		clients:   mp,
		logger:    logger,
		listeners: ls,
		closed:    cl,
	}
	assert.NoErrorf(t, err, "no error expected")
	assert.NoErrorf(t, err, "no error expected")

	var httpListener net.Listener
	httpListener, err = resolveAndListen(":7777", false, "", "", "", logger)
	assert.NoErrorf(t, err, "no error expected")

	prox.isConnected = true
	go func() {
		time.Sleep(5 * time.Second)
		close(prox.closed)
		httpListener.Close()
	}()
	err = prox.Serve(httpListener)
	assert.Errorf(t, err, "error expected")

}

type MockQueryPreparer struct {
	DeleteCalled bool
	DeleteParams struct {
		Raw *frame.RawFrame
		Msg *message.Prepare
		Id  [16]byte
	}
	InsertCalled bool
	InsertParams struct {
		Raw *frame.RawFrame
		Msg *message.Prepare
		Id  [16]byte
	}
	SelectCalled bool
	SelectParams struct {
		Raw *frame.RawFrame
		Msg *message.Prepare
		Id  [16]byte
	}
}

type MockClient struct {
	SendFunc func(header *frame.RawFrame, preparedResult *message.PreparedResult) error
	client
}

func (m *MockClient) Send(header *frame.RawFrame, preparedResult *message.PreparedResult) error {
	if m.SendFunc != nil {
		return m.SendFunc(header, preparedResult)
	}
	return nil
}

type mockSender struct {
	SendCalled bool
	SendParams struct {
		Hdr *frame.Header
		Msg message.Message
	}
}

func (m *mockSender) Send(hdr *frame.Header, msg message.Message) {
	m.SendCalled = true
	m.SendParams.Hdr = hdr
	m.SendParams.Msg = msg
}

var mockRawFrame = &frame.RawFrame{
	Header: &frame.Header{
		Version:  primitive.ProtocolVersion4,
		Flags:    0,
		StreamId: 0,
		OpCode:   primitive.OpCodePrepare,
	},
	Body: []byte{},
}
var schemaConfigs = &schemaMapping.SchemaMappingConfig{
	TablesMetaData:  mockTableSchemaConfig,
	PkMetadataCache: mockPkMetadata,
	Logger:          zap.NewNop(),
}

var mockProxy = &Proxy{
	schemaMapping: schemaConfigs,
	translator: &translator.Translator{
		SchemaMappingConfig: schemaConfigs,
		// todo remove once we support ordered code ints
		EncodeIntValuesWithBigEndian: false,
	},
	logger: zap.NewNop(),
}

// Create mock for handleExecutionForDeletePreparedQuery/handleExecutionForSelectPreparedQuery/handleExecutionForInsertPreparedQuery functions.
type MockBigtableClient struct {
	bigtableModule.BigtableClient
	InsertRowFunc          func(ctx context.Context, data *translator.InsertQueryMapping) error
	UpdateRowFunc          func(ctx context.Context, data *translator.UpdateQueryMapping) error
	DeleteRowFunc          func(ctx context.Context, data *translator.DeleteQueryMapping) error
	GetSchemaConfigsFunc   func(ctx context.Context, tableName string) (map[string]map[string]*schemaMapping.Column, map[string][]schemaMapping.Column, error)
	InsertErrorDetailsFunc func(ctx context.Context, query responsehandler.ErrorDetail)
	ApplyBulkMutationFunc  func(ctx context.Context, tableName string, mutationData []bt.MutationData, ar string) (bt.BulkOperationResponse, error)
}

func (m *MockBigtableClient) InsertRow(ctx context.Context, data *translator.InsertQueryMapping) error {
	if m.InsertRowFunc != nil {
		return m.InsertRowFunc(ctx, data)
	}
	return nil
}

func (m *MockBigtableClient) UpdateRow(ctx context.Context, data *translator.UpdateQueryMapping) error {
	if m.UpdateRowFunc != nil {
		return m.UpdateRowFunc(ctx, data)
	}
	return nil
}

func (m *MockBigtableClient) DeleteRow(ctx context.Context, data *translator.DeleteQueryMapping) error {
	if m.DeleteRowFunc != nil {
		return m.DeleteRowFunc(ctx, data)
	}
	return nil
}
func (m *MockBigtableClient) GetSchemaConfigs(ctx context.Context, tableName string) (map[string]map[string]*schemaMapping.Column, map[string][]schemaMapping.Column, error) {
	if m.GetSchemaConfigsFunc != nil {
		return m.GetSchemaConfigsFunc(ctx, tableName)
	}
	return nil, nil, nil
}

func (m *MockBigtableClient) InsertErrorDetails(ctx context.Context, query responsehandler.ErrorDetail) {
	if m.InsertErrorDetailsFunc != nil {
		m.InsertErrorDetailsFunc(ctx, query)
	}
}
func (m *MockBigtableClient) ApplyBulkMutation(ctx context.Context, tableName string, mutationData []bt.MutationData, ar string) (bt.BulkOperationResponse, error) {
	if m.ApplyBulkMutationFunc != nil {
		return m.ApplyBulkMutationFunc(ctx, tableName, mutationData, ar)
	}
	return bt.BulkOperationResponse{}, nil
}

var positionValues = []*primitive.Value{
	{Type: primitive.ValueTypeNull, Contents: []byte("value1")},
	{Type: primitive.ValueTypeNull, Contents: []byte("value2")},
}

var namedValues = map[string]*primitive.Value{
	"test_id":   {Type: primitive.ValueTypeNull, Contents: []byte("value1")},
	"test_hash": {Type: primitive.ValueTypeNull, Contents: []byte("value2")},
}

func Test_handleExecutionForDeletePreparedQuery(t *testing.T) {

	id := md5.Sum([]byte("DELETE FROM key_space.test_table WHERE test_id = '?'"))
	mockProxy := &Proxy{
		schemaMapping: schemaConfigs,
		translator: &translator.Translator{
			SchemaMappingConfig:          schemaConfigs,
			EncodeIntValuesWithBigEndian: false,
		},
		logger:  zap.NewNop(),
		ctx:     context.Background(),
		bClient: nil, // update client to include all function
		otelInst: &otelgo.OpenTelemetry{Config: &otelgo.OTelConfig{
			ServiceName: "test",
			OTELEnabled: false,
		}},
	}

	type fields struct {
		ctx                 context.Context
		proxy               *Proxy
		conn                *proxycore.Conn
		keyspace            string
		preparedSystemQuery map[[16]byte]interface{}
		preparedQuerys      map[[16]byte]interface{}
	}
	type args struct {
		raw           *frame.RawFrame
		msg           *partialExecute
		customPayload map[string][]byte
		st            *translator.DeleteQueryMapping
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{
			name: "Test handleExecutionForDeletePreparedQuery Query",
			fields: fields{
				ctx:   context.Background(),
				proxy: mockProxy,
			},

			args: args{
				raw: mockRawFrame,
				msg: &partialExecute{
					PositionalValues: positionValues,
					NamedValues:      namedValues,
					queryId:          id[:],
				},
				customPayload: map[string][]byte{
					"test_id":   []byte("1234"),
					"test_hash": []byte("1234-2345"),
				},
				st: &translator.DeleteQueryMapping{
					Table:     "test_table",
					Query:     "DELETE FROM key_space.test_table WHERE test_id = '?'",
					ParamKeys: []string{"test_id", "test_hash"},
					Keyspace:  "key_space",
					QueryType: "DELETE",
					Clauses: []translator.Clause{{
						Column:   "test_id",
						Operator: "=",
						Value:    "?",
					}},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSender := &mockSender{}
			c := &client{
				ctx:                 tt.fields.ctx,
				proxy:               tt.fields.proxy,
				conn:                tt.fields.conn,
				keyspace:            tt.fields.keyspace,
				preparedSystemQuery: tt.fields.preparedSystemQuery,
				preparedQuerys:      tt.fields.preparedQuerys,
				sender:              mockSender,
			}
			c.handleExecuteForDelete(tt.args.raw, tt.args.msg, tt.args.st, tt.fields.ctx)
			assert.Nil(t, c.preparedSystemQuery)
			assert.Nil(t, c.preparedQuerys)
		})
	}
}

type MockTranslator struct {
	mock.Mock
}

func (m *MockTranslator) TranslateInsertQuerytoBigtable(queryStr string, protocolV primitive.ProtocolVersion, isPreparedQuery bool) (*translator.InsertQueryMapping, error) {
	args := m.Called(queryStr)
	return args.Get(0).(*translator.InsertQueryMapping), args.Error(1)
}

func (m *MockTranslator) TranslateDeleteQuerytoBigtable(query string, isPreparedQuery bool) (*translator.DeleteQueryMapping, error) {
	args := m.Called(query)
	return args.Get(0).(*translator.DeleteQueryMapping), args.Error(1)
}
func (m *MockTranslator) TranslateUpdateQuerytoBigtable(query string, isPreparedQuery bool) (*translator.UpdateQueryMapping, error) {
	args := m.Called(query)
	return args.Get(0).(*translator.UpdateQueryMapping), args.Error(1)
}

type mockQuery struct{}

func TestHandleExecute(t *testing.T) {
	mockSender := &mockSender{}
	mockPartialExecute := &partialExecute{
		PositionalValues: positionValues,
		NamedValues:      namedValues,
	}
	mockSystemQueryID := preparedIdKey([]byte("some_prepared_query_id"))

	tests := []struct {
		name           string
		client         *client
		raw            *frame.RawFrame
		msg            *partialExecute
		setupClient    func(c *client)
		wantSendCalled bool
		wantSendMsg    message.Message
	}{
		{
			name: "System Query Execution",
			raw:  mockRawFrame,
			msg:  &partialExecute{queryId: []byte("some_prepared_query_id")},
			setupClient: func(c *client) {
				c.preparedSystemQuery = map[[16]byte]interface{}{
					mockSystemQueryID: &mockQuery{},
				}
			},
			wantSendCalled: true,
		},
		{
			name: "Prepared Query Execution",
			raw:  mockRawFrame,
			msg:  &partialExecute{queryId: []byte("some_other_prepared_query_id")},
			setupClient: func(c *client) {
				c.preparedQuerys = map[[16]byte]interface{}{
					preparedIdKey([]byte("some_other_prepared_query_id")): &translator.SelectQueryMap{},
				}
			},
			wantSendCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &client{
				proxy:               mockProxy,
				preparedSystemQuery: make(map[[16]byte]interface{}),
				preparedQuerys:      make(map[[16]byte]interface{}),
				sender:              mockSender,
			}
			if tt.setupClient != nil {
				tt.setupClient(c)
			}

			c.handleExecute(mockRawFrame, mockPartialExecute)
			if tt.wantSendCalled {
				if !mockSender.SendCalled {
					t.Errorf("Send was not called when expected for test: %s", tt.name)
				}
			} else {
				if mockSender.SendCalled {
					t.Errorf("Send was called when not expected for test: %s", tt.name)
				}
			}
		})
	}
}

func Test_client_handlePrepare(t *testing.T) {
	mockSender := &mockSender{}
	selectQuery := &message.Prepare{
		Query: "SELECT * FROM system.local WHERE key = 'local'",
	}
	useQuery := &message.Prepare{
		Query: "USE keyspace",
	}
	invalidQuery := &message.Prepare{
		Query: "INVALID QUERY",
	}

	tests := []struct {
		name           string
		client         *client
		raw            *frame.RawFrame
		msg            *message.Prepare
		setupClient    func(c *client)
		wantSendCalled bool
		wantSendMsg    message.Message
	}{
		{
			name: "Select Query",
			raw:  mockRawFrame,
			msg:  selectQuery,
			setupClient: func(c *client) {
				c.preparedSystemQuery = make(map[[16]byte]interface{})
				c.preparedQuerys = make(map[[16]byte]interface{})
			},
			wantSendCalled: true,
		},
		{
			name: "Use Query",
			raw:  mockRawFrame,
			msg:  useQuery,
			setupClient: func(c *client) {
				c.preparedSystemQuery = make(map[[16]byte]interface{})
				c.preparedQuerys = make(map[[16]byte]interface{})
			},
			wantSendCalled: true,
		},
		{
			name: "Invalid Query",
			raw:  mockRawFrame,
			msg:  invalidQuery,
			setupClient: func(c *client) {
				c.preparedSystemQuery = make(map[[16]byte]interface{})
				c.preparedQuerys = make(map[[16]byte]interface{})
			},
			wantSendCalled: true,
		},
		{
			name: "Select Statement with Invalid Table",
			raw:  mockRawFrame,
			msg:  selectQuery,
			setupClient: func(c *client) {
				c.preparedSystemQuery = make(map[[16]byte]interface{})
				c.preparedQuerys = make(map[[16]byte]interface{})
			},
			wantSendCalled: true,
			wantSendMsg:    &message.Invalid{},
		},
		{
			name: "Query Parsing Error",
			raw:  mockRawFrame,
			msg:  &message.Prepare{Query: "invalid query syntax"},
			setupClient: func(c *client) {
				c.preparedSystemQuery = make(map[[16]byte]interface{})
				c.preparedQuerys = make(map[[16]byte]interface{})
			},
			wantSendCalled: true,
			wantSendMsg:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &client{
				proxy:               mockProxy,
				preparedSystemQuery: make(map[[16]byte]interface{}),
				preparedQuerys:      make(map[[16]byte]interface{}),
				sender:              mockSender,
			}
			if tt.setupClient != nil {
				tt.setupClient(c)
			}

			c.handlePrepare(tt.raw, tt.msg)
			if tt.wantSendCalled {
				if !mockSender.SendCalled {
					t.Errorf("Send was not called when expected for test: %s", tt.name)
				}
			} else {
				if mockSender.SendCalled {
					t.Errorf("Send was called when not expected for test: %s", tt.name)
				}
			}
		})
	}
}

func createSchemaConfig() *schemaMapping.SchemaMappingConfig {
	return &schemaMapping.SchemaMappingConfig{
		TablesMetaData:     mockTableSchemaConfig,
		PkMetadataCache:    mockPkMetadata,
		SystemColumnFamily: "cf1",
	}
}

var mockTableSchemaConfig = map[string]map[string]map[string]*schemaMapping.Column{

	"keyspace": {
		"test_table": {
			"test_id": {
				ColumnName: "test_id",
				ColumnType: "text",
				Metadata: message.ColumnMetadata{
					Keyspace: "",
					Table:    "test_table",
					Name:     "test_id",
					Type:     datatype.Varchar,
					Index:    0,
				},
				IsPrimaryKey: true,
			},
			"test_hash": {
				ColumnName: "test_hash",
				ColumnType: "text",
				Metadata: message.ColumnMetadata{
					Keyspace: "",
					Table:    "test_table",
					Name:     "test_hash",
					Type:     datatype.Varchar,
					Index:    1,
				},
			},

			"column1": &schemaMapping.Column{
				ColumnName:   "column1",
				ColumnType:   "text",
				IsPrimaryKey: true,
				PkPrecedence: 1,
			},
			"column2": &schemaMapping.Column{
				ColumnName:   "column2",
				ColumnType:   "blob",
				IsPrimaryKey: false,
			},
			"column3": &schemaMapping.Column{
				ColumnName:   "column3",
				ColumnType:   "boolean",
				IsPrimaryKey: false,
			},

			"column10": &schemaMapping.Column{
				ColumnName:   "column10",
				ColumnType:   "text",
				IsPrimaryKey: true,
				PkPrecedence: 2,
			},
		},
		"user_info": {
			"name": &schemaMapping.Column{
				ColumnName:   "name",
				ColumnType:   "text",
				IsPrimaryKey: true,
				PkPrecedence: 0,
				IsCollection: false,
				Metadata: message.ColumnMetadata{
					Keyspace: "user_info",
					Table:    "user_info",
					Name:     "name",
					Index:    0,
					Type:     datatype.Varchar,
				},
			},
			"age": &schemaMapping.Column{
				ColumnName:   "age",
				ColumnType:   "text",
				IsPrimaryKey: false,
				PkPrecedence: 0,
				IsCollection: false,
				Metadata: message.ColumnMetadata{
					Keyspace: "user_info",
					Table:    "user_info",
					Name:     "age",
					Index:    1,
					Type:     datatype.Varchar,
				},
			},
		}},
}

func Test_detectEmptyPrimaryKey(t *testing.T) {
	type args struct {
		query *translator.InsertQueryMapping
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Primary key present and not null",
			args: args{
				query: &translator.InsertQueryMapping{
					Columns: []translator.Column{
						{Name: "id", IsPrimaryKey: true},
						{Name: "name", IsPrimaryKey: false},
					},
					Values: []interface{}{[]uint8{0, 0, 1}, "Alice"},
				},
			},
			want: "",
		},
		{
			name: "Primary key missing value",
			args: args{
				query: &translator.InsertQueryMapping{
					Columns: []translator.Column{
						{Name: "id", IsPrimaryKey: true},
						{Name: "name", IsPrimaryKey: false},
					},
					Values: []interface{}{nil, "Bob"},
				},
			},
			want: "id",
		},
		{
			name: "No primary key column",
			args: args{
				query: &translator.InsertQueryMapping{
					Columns: []translator.Column{
						{Name: "age", IsPrimaryKey: false},
						{Name: "name", IsPrimaryKey: false},
					},
					Values: []interface{}{25, "Eve"},
				},
			},
			want: "",
		},
		{
			name: "Multiple primary keys, one missing value",
			args: args{
				query: &translator.InsertQueryMapping{
					Columns: []translator.Column{
						{Name: "user_id", IsPrimaryKey: true},
						{Name: "product_id", IsPrimaryKey: true},
					},
					Values: []interface{}{nil, 101},
				},
			},
			want: "user_id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectEmptyPrimaryKey(tt.args.query); got != tt.want {
				t.Errorf("detectEmptyPrimaryKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Implement the methods from the BigtableClient that you need to mock
func (m *MockBigtableClient) GetSchemaMappingConfigs(ctx context.Context, instanceID, schemaMappingTable string) (map[string]map[string]*schemaMapping.Column, map[string][]schemaMapping.Column, error) {
	tbData := make(map[string]map[string]*schemaMapping.Column)
	pkData := make(map[string][]schemaMapping.Column)
	return tbData, pkData, nil
}
func TestNewProxy(t *testing.T) {
	ctx := context.Background()
	var logger *zap.Logger
	logger = proxycore.GetOrCreateNopLogger(logger)
	tbData := make(map[string]map[string]*schemaMapping.Column)
	pkData := make(map[string][]schemaMapping.Column)
	bgtmockface := new(mockbigtable.BigTableClientIface)
	bgtmockface.On("GetSchemaMappingConfigs", ctx, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(tbData, pkData, nil)
	bgtmockface.On("LoadConfigs", mock.AnythingOfType("*responsehandler.TypeHandler"), mock.AnythingOfType("*schemaMapping.SchemaMappingConfig")).Return(tbData, pkData, nil)

	// Override the factory function to return the mock
	originalNewBigTableClient := bt.NewBigtableClient
	bt.NewBigtableClient = func(client map[string]*bigtable.Client, adminClients map[string]*bigtable.AdminClient, logger *zap.Logger, sqlClient btpb.BigtableClient, config bt.BigtableConfig, responseHandler rh.ResponseHandlerIface, grpcConn *grpc.ClientConn, schemaMapping *schemaMapping.SchemaMappingConfig) bt.BigTableClientIface {
		return bgtmockface
	}
	defer func() { bt.NewBigtableClient = originalNewBigTableClient }()
	prox, err := NewProxy(ctx, Config{
		Version: primitive.ProtocolVersion4,
		Logger:  logger,
		OtelConfig: &OtelConfig{
			Enabled: false,
		},
		BigtableConfig: bt.BigtableConfig{},
	})
	assert.NotNilf(t, prox, "should not be nil")
	assert.NoErrorf(t, err, "should not through an error")
}

// Mock struct for prepared queries.
type MockQuery struct {
	VariableMetadata []*message.ColumnMetadata
	ReturnMetadata   []*message.ColumnMetadata
}

func (m *MockClient) GetQueryFromCache(id [16]byte) (interface{}, bool) {
	// Implement custom logic for mock cache retrieval based on your test needs
	return nil, false // Default response
}

func TestGetMetadataFromCache(t *testing.T) {
	ctx := context.Background()

	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
	}

	// Properly initialize preparedStatementID
	var preparedStatementID [16]byte
	copy(preparedStatementID[:], []byte("test"))

	// Case 1: When the query is not found in the cache.
	variableMeta, returnMeta, exists := client.getMetadataFromCache(preparedStatementID)
	assert.False(t, exists, "Expected false, should not find query in cache.")
	assert.Nil(t, variableMeta, "Expected variableMeta to be nil.")
	assert.Nil(t, returnMeta, "Expected returnMeta to be nil.")

	// Case 2: When the query is found for insert query
	insertQuery := "INSERT INTO products (product_id, product_name, price, category) VALUES (?, ?, ?, ?);"
	id := md5.Sum([]byte(insertQuery))
	client.AddQueryToCache(id, &translator.InsertQueryMapping{
		Query:     insertQuery,
		QueryType: "insert",
		Table:     "products",
		VariableMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
		ReturnMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
	})
	// Retrieve and assert
	variableMeta, returnMeta, exists = client.getMetadataFromCache(id)

	assert.True(t, exists, "Expected true, should find query in cache.")
	assert.NotNil(t, variableMeta, "Expected variableMeta to not be nil.")
	assert.NotNil(t, returnMeta, "Expected returnMeta to not be nil.")

	// Case 3: When the query is found for select query
	selectQuery := "SELECT id, name, age FROM users WHERE id = ?;"
	id = md5.Sum([]byte(selectQuery))
	client.AddQueryToCache(id, &translator.SelectQueryMap{
		Query:     insertQuery,
		QueryType: "select",
		Table:     "products",
		VariableMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
		ReturnMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
	})
	// Retrieve and assert
	variableMeta, returnMeta, exists = client.getMetadataFromCache(id)

	assert.True(t, exists, "Expected true, should find query in cache.")
	assert.NotNil(t, variableMeta, "Expected variableMeta to not be nil.")
	assert.NotNil(t, returnMeta, "Expected returnMeta to not be nil.")

	// Case 4: When the query is found for update query
	updateQuery := "UPDATE users SET age = ? WHERE id = ?;"
	id = md5.Sum([]byte(selectQuery))
	client.AddQueryToCache(id, &translator.UpdateQueryMapping{
		Query:     updateQuery,
		QueryType: "update",
		Table:     "products",
		VariableMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
		ReturnMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
	})
	// Retrieve and assert
	variableMeta, returnMeta, exists = client.getMetadataFromCache(id)

	assert.True(t, exists, "Expected true, should find query in cache.")
	assert.NotNil(t, variableMeta, "Expected variableMeta to not be nil.")
	assert.NotNil(t, returnMeta, "Expected returnMeta to not be nil.")

	// Case 5: When the query is found for delete query
	deleteQuery := "DELETE FROM products WHERE product_id = ?;"
	id = md5.Sum([]byte(selectQuery))
	client.AddQueryToCache(id, &translator.DeleteQueryMapping{
		Query:     deleteQuery,
		QueryType: "delete",
		Table:     "products",
		VariableMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
		ReturnMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
	})
	// Retrieve and assert
	variableMeta, returnMeta, exists = client.getMetadataFromCache(id)

	assert.True(t, exists, "Expected true, should find query in cache.")
	assert.NotNil(t, variableMeta, "Expected variableMeta to not be nil.")
	assert.NotNil(t, returnMeta, "Expected returnMeta to not be nil.")

	// Case 6: Invalid Query Type
	invalidQuery := "Invalid FROM products WHERE product_id = ?;"
	id = md5.Sum([]byte(invalidQuery))
	client.AddQueryToCache(id, mockQuery{})
	// Retrieve and assert
	variableMeta, returnMeta, exists = client.getMetadataFromCache(id)

	assert.False(t, exists, "Expected false, should fail to find query in cache.")
	assert.Nil(t, variableMeta, "Expected variableMeta to be nil.")
	assert.Nil(t, returnMeta, "Expected returnMeta to be nil.")
}

func TestHandleServerPreparedQuery(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			logger: mockProxy.logger,
		},
	}

	// Properly initialize preparedStatementID
	var preparedStatementID [16]byte
	copy(preparedStatementID[:], []byte("test"))

	insertQuery := "INSERT INTO products (product_id, product_name, price, category) VALUES (?, ?, ?, ?);"

	// Case 1: When the query is not found in the cache.
	client.handleServerPreparedQuery(mockRawFrame, &message.Prepare{Query: insertQuery,
		Keyspace: "test",
	}, "insert")
	if !mockSender.SendCalled {
		t.Errorf("Send was not called when expected for test - TestHandleServerPreparedQuery Case 1")
	}
	id := md5.Sum([]byte(insertQuery))
	client.AddQueryToCache(id, &translator.InsertQueryMapping{
		Query:     insertQuery,
		QueryType: "insert",
		Table:     "products",
		VariableMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
		ReturnMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
	})
	// Retrieve and assert
	client.handleServerPreparedQuery(mockRawFrame, &message.Prepare{Query: insertQuery}, "insert")
	if !mockSender.SendCalled {
		t.Errorf("Send was not called when expected for test - TestHandleServerPreparedQuery Case 2")
	}

	// Case 2: When the query is found for select query
	selectQuery := "SELECT * FROM users WHERE id = ?;"
	id = md5.Sum([]byte(selectQuery))
	client.AddQueryToCache(id, &translator.SelectQueryMap{
		Query:     selectQuery,
		QueryType: "select",
		Table:     "users",
		Keyspace:  "test",
		VariableMetadata: []*message.ColumnMetadata{{
			Table: "users",
		}},
		ReturnMetadata: []*message.ColumnMetadata{{
			Table: "users",
		}},
	})
	// Retrieve and assert
	client.handleServerPreparedQuery(mockRawFrame, &message.Prepare{Query: selectQuery,
		Keyspace: "test"}, "select")
	if !mockSender.SendCalled {
		t.Errorf("Send was not called when expected for test - TestHandleServerPreparedQuery Case 3")
	}

	// Case 3: When the query is found for update query
	updateQuery := "UPDATE users SET age = ? WHERE id = ?;"
	id = md5.Sum([]byte(selectQuery))
	client.AddQueryToCache(id, &translator.UpdateQueryMapping{
		Query:     updateQuery,
		QueryType: "update",
		Table:     "products",
		VariableMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
		ReturnMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
	})
	// Retrieve and assert
	client.handleServerPreparedQuery(mockRawFrame, &message.Prepare{Query: updateQuery}, "update")
	if !mockSender.SendCalled {
		t.Errorf("Send was not called when expected for test - TestHandleServerPreparedQuery Case 4")
	}
	// Case 4: When the query is found for delete query
	deleteQuery := "DELETE FROM products WHERE product_id = ?;"
	id = md5.Sum([]byte(selectQuery))
	client.AddQueryToCache(id, &translator.DeleteQueryMapping{
		Query:     deleteQuery,
		QueryType: "delete",
		Table:     "products",
		VariableMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
		ReturnMetadata: []*message.ColumnMetadata{{
			Table: "products",
		}},
	})
	// Retrieve and assert
	client.handleServerPreparedQuery(mockRawFrame, &message.Prepare{Query: deleteQuery}, "delete")
	if !mockSender.SendCalled {
		t.Errorf("Send was not called when expected for test - TestHandleServerPreparedQuery Case 5")
	}

	// Case 5: Invalid Query Type
	invalidQuery := "Invalid FROM products WHERE product_id = ?;"
	id = md5.Sum([]byte(invalidQuery))
	client.AddQueryToCache(id, mockQuery{})
	// Retrieve and assert
	client.handleServerPreparedQuery(mockRawFrame, &message.Prepare{Query: deleteQuery}, "invalid")
	if !mockSender.SendCalled {
		t.Errorf("Send was not called when expected for test - TestHandleServerPreparedQuery Case 6")
	}
}

func TestPrepareInsertType(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
		},
	}
	keyspace := ""
	insertQuery := "INSERT INTO test_keyspace.user_info (name, age) VALUES (?, ?);"
	id := md5.Sum([]byte(insertQuery + keyspace))
	returnMetadata, variableMetadata, err := client.prepareInsertType(mockRawFrame, &message.Prepare{
		Query:    insertQuery,
		Keyspace: keyspace,
	}, id)
	assert.NoError(t, err)
	assert.NotNil(t, returnMetadata)
	assert.NotNil(t, variableMetadata)
}

func TestPrepareSelectType(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
		},
	}
	keyspace := ""
	selectQuery := "SELECT name FROM test_keyspace.user_info WHERE name = ?;"
	id := md5.Sum([]byte(selectQuery + keyspace))
	returnMetadata, variableMetadata, err := client.prepareSelectType(mockRawFrame, &message.Prepare{
		Query:    selectQuery,
		Keyspace: keyspace,
	}, id)
	assert.NoError(t, err)
	assert.NotNil(t, returnMetadata)
	assert.NotNil(t, variableMetadata)
}

func TestPrepareUpdateType(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}
	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
		},
	}
	keyspace := ""
	updateQuery := "UPDATE test_keyspace.user_info SET age = ? WHERE name = ?;"
	id := md5.Sum([]byte(updateQuery + keyspace))
	returnMetadata, variableMetadata, err := client.prepareUpdateType(mockRawFrame, &message.Prepare{
		Query:    updateQuery,
		Keyspace: keyspace,
	}, id)
	assert.NoError(t, err)
	assert.NotNil(t, returnMetadata)
	assert.NotNil(t, variableMetadata)
}

func TestPrepareDeleteType(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
		},
	}
	keyspace := ""
	deleteQuery := "DELETE from test_keyspace.user_info  WHERE name = ?;"
	id := md5.Sum([]byte(deleteQuery + keyspace))
	returnMetadata, variableMetadata, err := client.prepareDeleteType(mockRawFrame, &message.Prepare{
		Query:    deleteQuery,
		Keyspace: keyspace,
	}, id)
	assert.NoError(t, err)
	assert.NotNil(t, returnMetadata)
	assert.NotNil(t, variableMetadata)
}

func TestHandleExecuteForInsert(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	bigTablemockiface := new(mockbigtable.BigTableClientIface)
	bigTablemockiface.On("InsertRow", ctx, &translator.InsertQueryMapping{Query: "INSERT INTO test_keyspace.user_info (name, age) VALUES (?, ?);", QueryType: "INSERT", Table: "user_info", Keyspace: "test_keyspace", Columns: []translator.Column{translator.Column{Name: "name", ColumnFamily: "", CQLType: "text", IsPrimaryKey: true}, translator.Column{Name: "age", ColumnFamily: "", CQLType: "text", IsPrimaryKey: false}}, Values: []interface{}{[]uint8{0x74, 0x65, 0x73, 0x74, 0x73, 0x68, 0x6f, 0x61, 0x69, 0x62}, []uint8{0x32, 0x35}}, Params: map[string]interface{}(nil), ParamKeys: []string(nil), PrimaryKeys: []string{"name"}, RowKey: "testshoaib", DeleteColumnFamilies: []string(nil), ReturnMetadata: []*message.ColumnMetadata(nil), VariableMetadata: []*message.ColumnMetadata(nil), TimestampInfo: translator.TimestampInfo{Timestamp: 0, HasUsingTimestamp: false, Index: 0}, IfNotExists: false}).Return(&message.RowsResult{}, nil)
	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			bClient:       bigTablemockiface,
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			otelInst: &otelgo.OpenTelemetry{Config: &otelgo.OTelConfig{
				ServiceName: "test",
				OTELEnabled: false,
			},
			},
		}}
	keyspace := ""
	insertQuery := "INSERT INTO test_keyspace.user_info (name, age) VALUES (?, ?);"
	id := md5.Sum([]byte(insertQuery + keyspace))
	varMeta, returnMeta, err := client.prepareInsertType(mockRawFrame, &message.Prepare{
		Query:    insertQuery,
		Keyspace: keyspace,
	}, id)
	assert.NoError(t, err)
	client.handleExecuteForInsert(mockRawFrame, &partialExecute{
		queryId: id[:],
		PositionalValues: []*primitive.Value{{
			Type:     0,
			Contents: []byte("testshoaib"),
		},
			{
				Type:     0,
				Contents: []byte("25"),
			}},
	}, &translator.InsertQueryMapping{
		Query:     insertQuery,
		QueryType: "INSERT",
		Table:     "user_info",
		Keyspace:  "test_keyspace",
		ParamKeys: []string{"name", "age"},
		Columns: []translator.Column{{
			Name:         "name",
			CQLType:      "text",
			IsPrimaryKey: true,
		}, {
			Name:         "age",
			CQLType:      "text",
			IsPrimaryKey: false}},
		VariableMetadata: varMeta,
		ReturnMetadata:   returnMeta,
		Values:           []interface{}{"testshoaib", "25"},
	}, ctx)
}

func TestHandleExecuteForSelect(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	bigTablemockiface := new(mockbigtable.BigTableClientIface)
	bigTablemockiface.On("SelectStatement", ctx, responsehandler.QueryMetadata{
		Query:                    "SELECT * FROM test_keyspace.user_info WHERE name = @name;",
		QueryType:                "SELECT",
		TableName:                "user_info",
		KeyspaceName:             "test_keyspace",
		ProtocalV:                0x4,
		Params:                   map[string]interface{}{"name": "testshoaib", "age": "testshoaib"},
		SelectedColumns:          nil,
		Paramkeys:                nil,
		ParamValues:              nil,
		UsingTSCheck:             "",
		SelectQueryForDelete:     "",
		PrimaryKeys:              []string{"name"},
		ComplexUpdateSelectQuery: "",
		UpdateSetValues:          nil,
		MutationKeyRange:         nil,
		AliasMap:                 nil,
		DefaultColumnFamily:      "cf1",
		IsStar:                   true,
		Limit:                    translator.Limit{IsLimit: false, Count: ""},
		IsGroupBy:                false,
	}).Return(&message.RowsResult{
		Metadata: &message.RowsMetadata{
			ColumnCount: 2,
			Columns: []*message.ColumnMetadata{
				{Name: "name", Type: datatype.Varchar},
				{Name: "age", Type: datatype.Varchar},
			},
		},
		Data: message.RowSet{},
	}, time.Now(), nil)

	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			bClient:       bigTablemockiface,
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			otelInst: &otelgo.OpenTelemetry{Config: &otelgo.OTelConfig{
				ServiceName: "test",
				OTELEnabled: false,
			}},
		}}
	keyspace := ""
	selectQuery := "SELECT * FROM test_keyspace.user_info WHERE name = ?;"
	id := md5.Sum([]byte(selectQuery + keyspace))
	varMeta, returnMeta, err := client.prepareSelectType(mockRawFrame, &message.Prepare{
		Query:    selectQuery,
		Keyspace: keyspace,
	}, id)
	assert.NoError(t, err)
	assert.NotNil(t, varMeta)
	assert.NotNil(t, returnMeta)

	// Create a SelectQueryMap with matching ParamKeys and VariableMetadata
	selectQueryMap := &translator.SelectQueryMap{
		Query:            selectQuery,
		QueryType:        "SELECT",
		Table:            "user_info",
		Keyspace:         "test_keyspace",
		ParamKeys:        make([]string, len(varMeta)), // Ensure ParamKeys has the same length as VariableMetadata
		VariableMetadata: varMeta,
		ReturnMetadata:   returnMeta,
		ColumnMeta:       translator.ColumnMeta{Star: true},
		TranslatedQuery:  "SELECT * FROM test_keyspace.user_info WHERE name = @name;",
		PrimaryKeys:      []string{"name"},
	}

	// Set the ParamKeys to match the VariableMetadata
	for i := range varMeta {
		selectQueryMap.ParamKeys[i] = varMeta[i].Name
	}

	// Create PositionalValues with the same length as VariableMetadata
	positionalValues := make([]*primitive.Value, len(varMeta))
	for i := range varMeta {
		positionalValues[i] = &primitive.Value{
			Type:     primitive.ValueTypeRegular,
			Contents: []byte("testshoaib"),
		}
	}

	client.handleExecuteForSelect(mockRawFrame, &partialExecute{
		queryId:          id[:],
		PositionalValues: positionalValues,
	}, selectQueryMap, ctx)

	assert.True(t, mockSender.SendCalled)
}

func TestHandleExecuteForDelete(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	bigTablemockiface := new(mockbigtable.BigTableClientIface)

	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			bClient:       bigTablemockiface,
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			otelInst: &otelgo.OpenTelemetry{Config: &otelgo.OTelConfig{
				ServiceName: "test",
				OTELEnabled: false,
			},
			},
		}}
	keyspace := ""
	deleteQuery := "DELETE FROM test_keyspace.user_info WHERE name = ?;"
	id := md5.Sum([]byte(deleteQuery + keyspace))
	varMeta, returnMeta, err := client.prepareDeleteType(mockRawFrame, &message.Prepare{
		Query:    deleteQuery,
		Keyspace: keyspace,
	}, id)
	t.Log("varMeta-0>¸", varMeta)
	assert.NoError(t, err)

	deleteQueryMap := &translator.DeleteQueryMapping{
		Query:            deleteQuery,
		QueryType:        "DELETE",
		Table:            "user_info",
		Keyspace:         "test_keyspace",
		ParamKeys:        []string{"name", "age"},
		VariableMetadata: varMeta,
		ReturnMetadata:   returnMeta,
	}
	bigTablemockiface.On("DeleteRow", ctx, deleteQueryMap).Return(&message.RowsResult{}, nil)
	client.handleExecuteForDelete(mockRawFrame, &partialExecute{
		queryId:          id[:],
		PositionalValues: []*primitive.Value{{Type: primitive.ValueTypeRegular, Contents: []byte("name")}, {Type: primitive.ValueTypeRegular, Contents: []byte("age")}},
	}, deleteQueryMap, ctx)
}

func TestHandleExecuteForUpdate(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	bigTablemockiface := new(mockbigtable.BigTableClientIface)
	bigTablemockiface.On("UpdateRow", ctx, &translator.UpdateQueryMapping{Query: "UPDATE test_keyspace.user_info SET age = ? WHERE name = ?;", QueryType: "UPDATE", Table: "user_info", Keyspace: "test_keyspace", Columns: []translator.Column{translator.Column{Name: "name", ColumnFamily: "", CQLType: "text", IsPrimaryKey: true}, translator.Column{Name: "age", ColumnFamily: "", CQLType: "text", IsPrimaryKey: false}}, Values: []interface{}{[]uint8{0x74, 0x65, 0x73, 0x74, 0x73, 0x68, 0x6f, 0x61, 0x69, 0x62}, []uint8{0x32, 0x35}}, Params: map[string]interface{}(nil), ParamKeys: []string(nil), PrimaryKeys: []string{"name"}, RowKey: "testshoaib", DeleteColumnFamilies: []string(nil), ReturnMetadata: []*message.ColumnMetadata(nil), VariableMetadata: []*message.ColumnMetadata(nil), TimestampInfo: translator.TimestampInfo{Timestamp: 0, HasUsingTimestamp: false, Index: 0}}).Return(&message.RowsResult{}, nil)
	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]any),
		sender:         mockSender,
		proxy: &Proxy{
			bClient:       bigTablemockiface,
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			otelInst: &otelgo.OpenTelemetry{Config: &otelgo.OTelConfig{
				ServiceName: "test",
				OTELEnabled: false,
			},
			},
		}}
	keyspace := ""
	updateQuery := "UPDATE test_keyspace.user_info SET age = ? WHERE name = ?;"
	id := md5.Sum([]byte(updateQuery + keyspace))
	varMeta, returnMeta, err := client.prepareUpdateType(mockRawFrame, &message.Prepare{
		Query:    updateQuery,
		Keyspace: keyspace,
	}, id)
	assert.NoError(t, err)
	client.handleExecuteForUpdate(mockRawFrame, &partialExecute{
		queryId: id[:],
		PositionalValues: []*primitive.Value{{
			Type:     0,
			Contents: []byte("testshoaib"),
		},
			{
				Type:     0,
				Contents: []byte("25"),
			}},
	}, &translator.UpdateQueryMapping{
		Query:     updateQuery,
		QueryType: "UPDATE",
		Table:     "user_info",
		Keyspace:  "test_keyspace",
		ParamKeys: []string{"name", "age"},
		Columns: []translator.Column{{
			Name:         "name",
			CQLType:      "text",
			IsPrimaryKey: true,
		}, {
			Name:         "age",
			CQLType:      "text",
			IsPrimaryKey: false}},
		VariableMetadata: varMeta,
		ReturnMetadata:   returnMeta,
		Values:           []interface{}{"testshoaib", "25"},
	}, ctx)
}

func TestHandleQueryInsert(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	bigTablemockiface := new(mockbigtable.BigTableClientIface)
	bigTablemockiface.On("InsertRow", nil, &translator.InsertQueryMapping{Query: "INSERT INTO test_keyspace.user_info (name, age) VALUES ('shoaib', '32');", QueryType: "INSERT", Table: "user_info", Keyspace: "test_keyspace", Columns: []translator.Column{translator.Column{Name: "name", ColumnFamily: "cf1", CQLType: "text", IsPrimaryKey: true}, translator.Column{Name: "age", ColumnFamily: "cf1", CQLType: "text", IsPrimaryKey: false}}, Values: []interface{}{[]uint8{0x73, 0x68, 0x6f, 0x61, 0x69, 0x62}, []uint8{0x33, 0x32}}, Params: map[string]interface{}{"age": []uint8{0x33, 0x32}, "name": []uint8{0x73, 0x68, 0x6f, 0x61, 0x69, 0x62}}, ParamKeys: []string{"name", "age"}, PrimaryKeys: []string{"name"}, RowKey: "shoaib", DeleteColumnFamilies: []string(nil), ReturnMetadata: []*message.ColumnMetadata(nil), VariableMetadata: []*message.ColumnMetadata(nil), TimestampInfo: translator.TimestampInfo{Timestamp: 0, HasUsingTimestamp: false, Index: 0}, IfNotExists: false}).Return(&message.RowsResult{}, nil)
	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			bClient:       bigTablemockiface,
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			otelInst: &otelgo.OpenTelemetry{Config: &otelgo.OTelConfig{
				ServiceName: "test",
				OTELEnabled: false,
			},
			},
		}}

	insertQuery := "INSERT INTO test_keyspace.user_info (name, age) VALUES ('shoaib', '32');"
	client.handleQuery(mockRawFrame, &partialQuery{
		query: insertQuery,
	})
}

func TestHandleQueryUpdate(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	bigTablemockiface := new(mockbigtable.BigTableClientIface)
	bigTablemockiface.On("UpdateRow", nil, &translator.UpdateQueryMapping{Query: "UPDATE test_keyspace.user_info SET age = '33' WHERE name = 'ibrahim';", TranslatedQuery: "", QueryType: "UPDATE", Table: "user_info", Keyspace: "test_keyspace", UpdateSetValues: []translator.UpdateSetValue{translator.UpdateSetValue{Column: "age", Value: "@set1", ColumnFamily: "", CQLType: "text", Encrypted: []uint8{0x33, 0x33}}}, Clauses: []translator.Clause{translator.Clause{Column: "name", Operator: "=", Value: "@value1", IsPrimaryKey: true}}, Params: map[string]interface{}{"set1": []uint8{0x33, 0x33}, "value1": "ibrahim"}, ParamKeys: []string{"set1", "value1"}, PrimaryKeys: []string{"name"}, Columns: []translator.Column{translator.Column{Name: "age", ColumnFamily: "cf1", CQLType: "text", IsPrimaryKey: false}, translator.Column{Name: "name", ColumnFamily: "cf1", CQLType: "text", IsPrimaryKey: false}}, Values: []interface{}{[]uint8{0x33, 0x33}, []uint8{0x69, 0x62, 0x72, 0x61, 0x68, 0x69, 0x6d}}, RowKey: "ibrahim", DeleteColumnFamilies: []string(nil), DeleteColumQualifires: []translator.Column(nil), ReturnMetadata: []*message.ColumnMetadata(nil), VariableMetadata: []*message.ColumnMetadata(nil), TimestampInfo: translator.TimestampInfo{Timestamp: 0, HasUsingTimestamp: false, Index: 0}, IfExists: false, ComplexOperation: map[string]*translator.ComplexOperation{}}).Return(&message.RowsResult{}, nil)
	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			bClient:       bigTablemockiface,
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			otelInst: &otelgo.OpenTelemetry{Config: &otelgo.OTelConfig{
				ServiceName: "test",
				OTELEnabled: false,
			},
			},
		}}

	updateQuery := "UPDATE test_keyspace.user_info SET age = '33' WHERE name = 'ibrahim';"
	client.handleQuery(mockRawFrame, &partialQuery{
		query: updateQuery,
	})
}

func TestHandleQueryDelete(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	deleteQuery := "DELETE FROM test_keyspace.user_info WHERE name = 'ibrahim';"
	bigTablemockiface := new(mockbigtable.BigTableClientIface)
	bigTablemockiface.On("DeleteRowNew", nil, &translator.DeleteQueryMapping{Query: "DELETE FROM test_keyspace.user_info WHERE name = 'ibrahim';", QueryType: "DELETE", Table: "user_info", Keyspace: "test_keyspace", Clauses: []translator.Clause{translator.Clause{Column: "name", Operator: "=", Value: "ibrahim", IsPrimaryKey: true}}, Params: map[string]interface{}{"value1": []uint8{0x69, 0x62, 0x72, 0x61, 0x68, 0x69, 0x6d}}, ParamKeys: []string{"value1"}, PrimaryKeys: []string{"name"}, RowKey: "ibrahim", ExecuteByMutation: false, VariableMetadata: []*message.ColumnMetadata(nil), ReturnMetadata: []*message.ColumnMetadata(nil), TimestampInfo: translator.TimestampInfo{Timestamp: 0, HasUsingTimestamp: false, Index: 0}, IfExists: false, SelectedColumns: []schemaMapping.SelectedColumns(nil)}).Return(&message.RowsResult{}, nil)
	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			bClient:       bigTablemockiface,
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			otelInst: &otelgo.OpenTelemetry{Config: &otelgo.OTelConfig{
				ServiceName: "test",
				OTELEnabled: false,
			},
			},
		}}
	client.handleQuery(mockRawFrame, &partialQuery{
		query: deleteQuery,
	})
}

func TestGetBigtableSqlConnection(t *testing.T) {
	// Create an in-memory gRPC server using bufconn.
	bgClient, clientCon, err := createBigtableSqlConnection(mockProxy.ctx, Config{})
	assert.NoError(t, err)
	assert.NotNil(t, bgClient)
	assert.NotNil(t, clientCon)
}

func TestHandleBatchUpdate(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	updateQuery := "UPDATE test_keyspace.user_info SET age = ? WHERE name = ?;"
	bigTablemockiface := new(mockbigtable.BigTableClientIface)
	bigTablemockiface.On("ApplyBulkMutation", nil, "user_info", []bt.MutationData{bt.MutationData{MutationType: "Update", RowKey: "value2", Columns: []bt.ColumnData{bt.ColumnData{ColumnFamily: "cf1", Name: "age", Contents: []uint8{0x76, 0x61, 0x6c, 0x75, 0x65, 0x31}}}, ColumnFamily: ""}}, "test_keyspace").Return(bt.BulkOperationResponse{}, nil)
	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			bClient:       bigTablemockiface,
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			otelInst: &otelgo.OpenTelemetry{Config: &otelgo.OTelConfig{
				ServiceName: "test",
				OTELEnabled: false,
			},
			},
		}}
	keyspace := ""

	id := md5.Sum([]byte(updateQuery + keyspace))
	_, _, err := client.prepareUpdateType(mockRawFrame, &message.Prepare{
		Query:    updateQuery,
		Keyspace: keyspace,
	}, id)
	assert.NoError(t, err)
	client.handleBatch(mockRawFrame, &partialBatch{
		queryOrIds:            []interface{}{id[:]},
		BatchPositionalValues: [][]*primitive.Value{positionValues},
	})
}

func TestHandleBatchInsert(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	insertQuery := "INSERT INTO test_keyspace.user_info (name, age) VALUES (?, ?);"
	bigTablemockiface := new(mockbigtable.BigTableClientIface)
	bigTablemockiface.On("ApplyBulkMutation", nil, "user_info", []bt.MutationData{bt.MutationData{MutationType: "Insert", RowKey: "value1", Columns: []bt.ColumnData{bt.ColumnData{ColumnFamily: "cf1", Name: "name", Contents: []uint8{0x76, 0x61, 0x6c, 0x75, 0x65, 0x31}}, bt.ColumnData{ColumnFamily: "cf1", Name: "age", Contents: []uint8{0x76, 0x61, 0x6c, 0x75, 0x65, 0x32}}}, ColumnFamily: ""}}, "test_keyspace").Return(bt.BulkOperationResponse{}, nil)
	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			bClient:       bigTablemockiface,
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			otelInst: &otelgo.OpenTelemetry{Config: &otelgo.OTelConfig{
				ServiceName: "test",
				OTELEnabled: false,
			},
			},
		}}
	keyspace := ""
	id := md5.Sum([]byte(insertQuery + keyspace))
	_, _, err := client.prepareInsertType(mockRawFrame, &message.Prepare{
		Query:    insertQuery,
		Keyspace: keyspace,
	}, id)
	assert.NoError(t, err)
	client.handleBatch(mockRawFrame, &partialBatch{
		queryOrIds:            []interface{}{id[:]},
		BatchPositionalValues: [][]*primitive.Value{positionValues},
	})
}

func TestHandleBatchSelect(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	var mutationData []bt.MutationData
	selectQuery := "SELECT * FROM test_keyspace.user_info WHERE name = ?;"
	bigTablemockiface := new(mockbigtable.BigTableClientIface)
	bigTablemockiface.On("ApplyBulkMutation", nil, "", mutationData, "").Return(bt.BulkOperationResponse{}, nil)
	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			bClient:       bigTablemockiface,
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			otelInst: &otelgo.OpenTelemetry{Config: &otelgo.OTelConfig{
				ServiceName: "test",
				OTELEnabled: false,
			},
			},
		}}
	keyspace := ""

	id := md5.Sum([]byte(selectQuery + keyspace))
	_, _, err := client.prepareSelectType(mockRawFrame, &message.Prepare{
		Query:    selectQuery,
		Keyspace: keyspace,
	}, id)
	assert.NoError(t, err)
	client.handleBatch(mockRawFrame, &partialBatch{
		queryOrIds:            []interface{}{id[:]},
		BatchPositionalValues: [][]*primitive.Value{positionValues},
	})
}

func TestHandleBatchDelete(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	deleteQuery := "DELETE FROM test_keyspace.user_info WHERE name = ?;"
	bigTablemockiface := new(mockbigtable.BigTableClientIface)
	bigTablemockiface.On("ApplyBulkMutation", nil, "user_info", []bt.MutationData{bt.MutationData{MutationType: "Delete", RowKey: "value1", Columns: []bt.ColumnData{bt.ColumnData{ColumnFamily: "cf1", Name: "name", Contents: []uint8{0x76, 0x61, 0x6c, 0x75, 0x65, 0x31}}, bt.ColumnData{ColumnFamily: "cf1", Name: "age", Contents: []uint8{0x76, 0x61, 0x6c, 0x75, 0x65, 0x32}}}, ColumnFamily: ""}}, "test_keyspace").Return(bt.BulkOperationResponse{}, nil)
	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			bClient:       bigTablemockiface,
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			otelInst: &otelgo.OpenTelemetry{Config: &otelgo.OTelConfig{
				ServiceName: "test",
				OTELEnabled: false,
			},
			},
		}}
	keyspace := ""
	id := md5.Sum([]byte(deleteQuery + keyspace))
	_, _, err := client.prepareDeleteType(mockRawFrame, &message.Prepare{
		Query:    deleteQuery,
		Keyspace: keyspace,
	}, id)
	assert.NoError(t, err)
	client.handleBatch(mockRawFrame, &partialBatch{
		queryOrIds:            []interface{}{id[:]},
		BatchPositionalValues: [][]*primitive.Value{positionValues},
	})
}

func TestClient_Receive(t *testing.T) {
	// Mock dependencies
	sender := new(mockSender)
	proxy := mockProxy
	proxy.logger = zap.NewNop() // No-op logger for testing

	// Create client with mocks
	client := &client{
		sender: sender,
		proxy:  proxy,
	}

	// Test case 1: DecodeRawFrame error (io.EOF)
	t.Run("DecodeRawFrame error (io.EOF)", func(t *testing.T) {
		reader := bytes.NewReader([]byte{}) // Empty reader to simulate EOF
		err := client.Receive(reader)
		assert.ErrorIs(t, err, io.EOF)
	})
	// Test case 2: DecodeRawFrame error (other)
	t.Run("DecodeRawFrame error (other)", func(t *testing.T) {
		reader := bytes.NewReader([]byte{0x01, 0x02, 0x03}) // Invalid data
		err := client.Receive(reader)
		assert.Error(t, err)
	})
}

func TestGetTimestampMetadata(t *testing.T) {
	timestampColumnName := "ts_column"
	// Test cases
	tests := []struct {
		name             string
		inputMetadata    translator.InsertQueryMapping
		initialMetadata  []*message.ColumnMetadata
		expectedMetadata []*message.ColumnMetadata
	}{
		{
			name: "With timestamp",
			inputMetadata: translator.InsertQueryMapping{
				Keyspace: "test_keyspace",
				Table:    "test_table",
				TimestampInfo: translator.TimestampInfo{
					HasUsingTimestamp: true,
					Index:             1,
				},
			},
			initialMetadata: []*message.ColumnMetadata{},
			expectedMetadata: []*message.ColumnMetadata{
				{
					Keyspace: "test_keyspace",
					Table:    "test_table",
					Name:     timestampColumnName,
					Index:    1,
					Type:     datatype.Bigint,
				},
			},
		},
		{
			name: "Without timestamp",
			inputMetadata: translator.InsertQueryMapping{
				Keyspace: "test_keyspace",
				Table:    "test_table",
				TimestampInfo: translator.TimestampInfo{
					HasUsingTimestamp: false,
				},
			},
			initialMetadata:  []*message.ColumnMetadata{},
			expectedMetadata: []*message.ColumnMetadata{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputMetadata := getTimestampMetadata(tt.inputMetadata, tt.initialMetadata)
			assert.Equal(t, tt.expectedMetadata, outputMetadata)
		})
	}
}

func TestClose(t *testing.T) {
	bigTablemockiface := new(mockbigtable.BigTableClientIface)
	bigTablemockiface.On("Close").Return()

	proxy := &Proxy{
		schemaMapping: GetSchemaMappingConfig(),
		logger:        mockProxy.logger,
		translator: &translator.Translator{
			SchemaMappingConfig: GetSchemaMappingConfig(),
		},
		closed:  make(chan struct{}),
		bClient: bigTablemockiface,
	}
	err := proxy.Close()
	assert.NoError(t, err)
}

func TestIsIdempotent(t *testing.T) {
	// Create a zap logger for testing
	logger := proxycore.GetOrCreateNopLogger(nil)

	// Initialize the Proxy with a sync.Map and logger
	proxy := &Proxy{
		preparedIdempotence: sync.Map{},
		logger:              logger,
	}

	// Test data
	id := []byte{0x01, 0x02, 0x03, 0x04}

	// Scenario 1: preparedIdempotence has no entry for the given id
	assert.False(t, proxy.IsIdempotent(id), "Expected IsIdempotent to return false for missing idempotence entry")

	// Scenario 2: preparedIdempotence has an entry for the given id
	proxy.preparedIdempotence.Store(preparedIdKey(id), true)
	assert.True(t, proxy.IsIdempotent(id), "Expected IsIdempotent to return true for idempotent entry")

	// Scenario 3: preparedIdempotence has a non-idempotent entry for the given id
	proxy.preparedIdempotence.Store(preparedIdKey(id), false)
	assert.False(t, proxy.IsIdempotent(id), "Expected IsIdempotent to return false for non-idempotent entry")
}

func TestFilterSystemLocalValues(t *testing.T) {
	mockProxy := mockProxy
	mockProxy.cluster = &proxycore.Cluster{
		NegotiatedVersion: primitive.ProtocolVersion4,
	}
	mockProxy.systemLocalValues = map[string]message.Column{
		"example_column": []byte(""),
	}
	client := &client{
		ctx:   context.TODO(),
		proxy: mockProxy,
	}

	stmt := &parser.SelectStatement{
		Keyspace: "test_keyspace",
		Table:    "test_table",
		Selectors: []parser.Selector{
			&parser.CountStarSelector{Name: "rpc_address"},
			&parser.CountStarSelector{Name: "host_id"},
			&parser.CountStarSelector{Name: "example_column"},
			&parser.CountStarSelector{Name: parser.CountValueName},
		},
	}

	filtered := []*message.ColumnMetadata{
		{Name: "rpc_address"},
		{Name: "host_id"},
		{Name: "example_column"},
		{Name: parser.CountValueName},
	}
	_, err := client.filterSystemLocalValues(stmt, filtered)
	assert.NoError(t, err)
}

func TestFilterSystemPeerValues(t *testing.T) {
	mockProxy := mockProxy
	mockProxy.cluster = &proxycore.Cluster{
		NegotiatedVersion: primitive.ProtocolVersion4,
	}
	mockNode := &node{
		dc:     "example_dc",
		addr:   &net.IPAddr{IP: []byte("127.0.1")},
		tokens: []string{"token1", "token2"},
	}

	client := &client{
		ctx:   context.TODO(),
		proxy: mockProxy,
	}

	stmt := &parser.SelectStatement{
		Keyspace: "test_keyspace",
		Table:    "test_table",
		Selectors: []parser.Selector{
			&parser.CountStarSelector{Name: "data_center"},
			&parser.CountStarSelector{Name: "host_id"},
			&parser.CountStarSelector{Name: "tokens"},
			&parser.CountStarSelector{Name: "peer"},
			&parser.CountStarSelector{Name: "rpc_address"},
			&parser.CountStarSelector{Name: "example_column"},
			&parser.CountStarSelector{Name: parser.CountValueName},
		},
	}
	filtered := []*message.ColumnMetadata{
		{Name: "data_center"},
		{Name: "host_id"},
		{Name: "tokens"},
		{Name: "peer"},
		{Name: "rpc_address"},
		{Name: "example_column"},
		{Name: parser.CountValueName},
	}

	_, err := client.filterSystemPeerValues(stmt, filtered, mockNode, 5)
	assert.NoError(t, err)
}

func TestDefaultPreparedCache(t *testing.T) {
	capacity := 10
	lruCache, _ := lru.New(capacity) // This should be from the actual LRU implementation you use.
	defaultCache := defaultPreparedCache{cache: lruCache}

	// Create a dummy PreparedEntry for testing
	preparedEntry := &proxycore.PreparedEntry{
		PreparedFrame: &frame.RawFrame{}, // Initialize as needed
	}

	// Store a value
	id := "test-id"
	defaultCache.Store(id, preparedEntry)

	// Now load the value
	loadedEntry, ok := defaultCache.Load(id)

	// Assertions
	assert.True(t, ok, "expected entry to be found")
	assert.Equal(t, preparedEntry, loadedEntry, "loaded entry should be same as stored entry")

	// Try to load a non-existent entry
	_, ok = defaultCache.Load("non-existent-id")
	assert.False(t, ok, "expected entry to not be found")
}

func TestGetTimestampMetadataForUpdate(t *testing.T) {
	// Test Case 1: With Timestamp
	updateQueryMetadata := translator.UpdateQueryMapping{
		Keyspace: "test_keyspace",
		Table:    "test_table",
		TimestampInfo: translator.TimestampInfo{
			HasUsingTimestamp: true,
			Index:             0,
		},
	}
	existingColumns := []*message.ColumnMetadata{
		{
			Keyspace: "test_keyspace",
			Table:    "test_table",
			Name:     "existing_column",
			Index:    1,
			Type:     datatype.Varchar, // Assuming String is a valid type.
		},
	}

	result := getTimestampMetadataForUpdate(updateQueryMetadata, existingColumns)

	// Assertions for Test Case 1
	assert.Len(t, result, 2, "Expected two columns in the result")
	assert.Equal(t, "test_keyspace", result[0].Keyspace)
	assert.Equal(t, "test_table", result[0].Table)
	assert.Equal(t, TimestampColumnName, result[0].Name)
	assert.Equal(t, int32(0), result[0].Index)         // Assuming Index is of type uint32
	assert.Equal(t, datatype.Bigint, result[0].Type)   // Assuming Bigint is a valid type
	assert.Equal(t, "existing_column", result[1].Name) // Check the existing column is still present

	// Test Case 2: Without Timestamp
	updateQueryMetadata.TimestampInfo.HasUsingTimestamp = false
	result = getTimestampMetadataForUpdate(updateQueryMetadata, existingColumns)

	// Assertions for Test Case 2
	assert.Len(t, result, 1, "Expected one column in the result (only existing column)")
	assert.Equal(t, "existing_column", result[0].Name) // Only existing column should be present
}

func TestRemoveClient(t *testing.T) {
	cd := &client{}
	cl := make(map[*client]struct{})

	proxy := &Proxy{
		clients: cl,
	}
	proxy.registerForEvents(cd)
	proxy.removeClient(cd)
}

func TestHandleQuerySelect(t *testing.T) {
	ctx := context.Background()
	mockSender := &mockSender{}

	bigTablemockiface := new(mockbigtable.BigTableClientIface)
	bigTablemockiface.On("SelectStatement", nil, responsehandler.QueryMetadata{Query: "SELECT * FROM user_info WHERE name = @value1;", QueryType: "", TableName: "user_info", KeyspaceName: "test_keyspace", ProtocalV: 0x4, Params: map[string]interface{}{"value1": "shoaib"}, SelectedColumns: []schemaMapping.SelectedColumns(nil), Paramkeys: []string(nil), ParamValues: []interface{}(nil), UsingTSCheck: "", SelectQueryForDelete: "", PrimaryKeys: []string(nil), ComplexUpdateSelectQuery: "", UpdateSetValues: []translator.UpdateSetValue(nil), MutationKeyRange: []interface{}(nil), AliasMap: map[string]translator.AsKeywordMeta(nil), DefaultColumnFamily: "cf1", IsStar: true, Limit: translator.Limit{IsLimit: false, Count: ""}, IsGroupBy: false}).Return(&message.RowsResult{}, time.Now(), nil)
	client := client{
		ctx:            ctx,
		preparedQuerys: make(map[[16]byte]interface{}),
		sender:         mockSender,
		proxy: &Proxy{
			bClient:       bigTablemockiface,
			schemaMapping: GetSchemaMappingConfig(),
			logger:        mockProxy.logger,
			translator: &translator.Translator{
				SchemaMappingConfig: GetSchemaMappingConfig(),
			},
			otelInst: &otelgo.OpenTelemetry{Config: &otelgo.OTelConfig{
				ServiceName: "test",
				OTELEnabled: false,
			},
			},
		}}

	selectQuery := "SELECT * user_info WHERE name = 'shoaib';"
	client.handleQuery(mockRawFrame, &partialQuery{
		query: selectQuery,
	})
}
