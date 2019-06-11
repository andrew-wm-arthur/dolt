package dbfactory

import (
	"context"
	"fmt"
	"github.com/attic-labs/noms/go/datas"
	"github.com/liquidata-inc/ld/dolt/go/libraries/utils/earl"
	"net/url"
	"strings"
)

const (
	// AWSScheme
	AWSScheme = "aws"

	// GSScheme
	GSScheme = "gs"

	// FileScheme
	FileScheme = "file"

	// MemScheme
	MemScheme = "mem"

	// HTTPSScheme
	HTTPSScheme = "https"

	// HTTPScheme
	HTTPScheme = "http"

	defaultScheme       = HTTPSScheme
	defaultMemTableSize = 256 * 1024 * 1024
)

// DBFactory is an interface for creating concrete datas.Database instances which may have different backing stores.
type DBFactory interface {
	CreateDB(ctx context.Context, urlObj *url.URL, params map[string]string) (datas.Database, error)
}

// DBFactories is a map from url scheme name to DBFactory.  Additional factories can be added to the DBFactories map
// from external packages.
var DBFactories = map[string]DBFactory{
	AWSScheme:  AWSFactory{},
	GSScheme:   GSFactory{},
	FileScheme: FileFactory{},
	MemScheme:  MemFactory{},
}

// InitializeFactories initializes any factories that rely on a GRPCConnectionProvider (Namely http and https)
func InitializeFactories(grpcCP GRPCConnectionProvider) {
	DBFactories[HTTPScheme] = NewDoltRemoteFactory(grpcCP, true)
	DBFactories[HTTPSScheme] = NewDoltRemoteFactory(grpcCP, false)
}

// CreateDB creates a database based on the supplied urlStr, and creation params.  The DBFactory used for creation is
// determined by the scheme of the url.  Naked urls will use https by default.
func CreateDB(ctx context.Context, urlStr string, params map[string]string) (datas.Database, error) {
	urlObj, err := earl.Parse(urlStr)

	if err != nil {
		return nil, err
	}

	scheme := urlObj.Scheme
	if len(scheme) == 0 {
		scheme = defaultScheme
	}

	if fact, ok := DBFactories[strings.ToLower(scheme)]; ok {
		return fact.CreateDB(ctx, urlObj, params)
	}

	return nil, fmt.Errorf("unknown url scheme: '%s'", urlObj.Scheme)
}