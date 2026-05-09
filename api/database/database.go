package database

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/photoview/photoview/api/database/drivers"
	"github.com/photoview/photoview/api/database/migrations"
	"github.com/photoview/photoview/api/graphql/models"
	"github.com/photoview/photoview/api/log"
	"github.com/photoview/photoview/api/utils"

	"github.com/go-sql-driver/mysql"
	gorm_mysql "gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type dbKeyType string

var dbKey dbKeyType = "db"

func WithDB(ctx context.Context, db *gorm.DB) context.Context {
	return context.WithValue(ctx, dbKey, db)
}

func DB(ctx context.Context) *gorm.DB {
	v := ctx.Value(dbKey)
	if v == nil {
		return nil
	}

	db, ok := v.(*gorm.DB)
	if !ok {
		log.Warn(ctx, "Invalid DB type", "type", fmt.Sprintf("%T", v))
	}

	return db
}

func GetMysqlAddress(addressString string) (string, error) {
	if addressString == "" {
		return "", fmt.Errorf("Environment variable %s missing, exiting", utils.EnvMysqlURL.GetName())
	}

	config, err := mysql.ParseDSN(addressString)
	if err != nil {
		return "", fmt.Errorf("could not parse mysql url: %w", err)
	}

	config.MultiStatements = true
	config.ParseTime = true

	return config.FormatDSN(), nil
}

func GetPostgresAddress(addressString string) (*url.URL, error) {
	if addressString == "" {
		return nil, fmt.Errorf("Environment variable %s missing, exiting", utils.EnvPostgresURL.GetName())
	}

	address, err := url.Parse(addressString)
	if err != nil {
		return nil, fmt.Errorf("could not parse postgres url: %w", err)
	}

	return address, nil
}

func GetSqliteAddress(path string) (*url.URL, error) {
	if path == "" {
		path = "photoview.db"
	}

	address, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("could not parse sqlite url (%s): %w", path, err)
	}

	queryValues := address.Query()
	queryValues.Add("cache", "shared")
	queryValues.Add("mode", "rwc")
	// queryValues.Add("_busy_timeout", "60000") // 1 minute
	queryValues.Add("_journal_mode", "WAL")    // Write-Ahead Logging (WAL) mode
	queryValues.Add("_locking_mode", "NORMAL") // allows concurrent reads and writes
	queryValues.Add("_foreign_keys", "ON")     // Enforc foreign key constraints.
	address.RawQuery = queryValues.Encode()

	return address, nil
}

func ConfigureDatabase(config *gorm.Config) (*gorm.DB, error) {
	var databaseDialect gorm.Dialector
	driver := drivers.DatabaseDriverFromEnv()
	log.Info(nil, "database driver based on environment variables", "driver", driver)

	switch driver {
	case drivers.MYSQL:
		mysqlAddress, err := GetMysqlAddress(utils.EnvMysqlURL.GetValue())
		if err != nil {
			return nil, err
		}
		databaseDialect = gorm_mysql.Open(mysqlAddress)
	case drivers.SQLITE:
		sqliteAddress, err := GetSqliteAddress(utils.EnvSqlitePath.GetValue())
		if err != nil {
			return nil, err
		}
		databaseDialect = sqlite.Open(sqliteAddress.String())

	case drivers.POSTGRES:
		postgresAddress, err := GetPostgresAddress(utils.EnvPostgresURL.GetValue())
		if err != nil {
			return nil, err
		}
		databaseDialect = postgres.Open(postgresAddress.String())
	}

	db, err := gorm.Open(databaseDialect, config)
	if err != nil {
		return nil, err
	}

	return db, nil
}

// SetupDatabase connects to the database using environment variables
func SetupDatabase() (*gorm.DB, error) {

	config := gorm.Config{}

	// Configure database logging
	if utils.DevelopmentMode() {
		config.Logger = logger.Default.LogMode(logger.Info)
	} else {
		config.Logger = logger.Default.LogMode(logger.Warn)
	}

	var db *gorm.DB

	for retryCount := 1; retryCount <= 5; retryCount++ {

		var err error
		db, err = ConfigureDatabase(&config)
		if err == nil {
			sqlDB, dbErr := db.DB()
			if dbErr != nil {
				return nil, dbErr
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = sqlDB.PingContext(ctx)
			cancel()

			sqlDB.SetMaxOpenConns(80)

			if err == nil {
				return db, nil
			}
		}

		log.Warn(nil, "Could not ping database. Will retry after 5 seconds", "error", err)
		time.Sleep(time.Duration(5) * time.Second)
	}

	return db, nil
}

var database_models []interface{} = []interface{}{
	&models.User{},
	&models.AccessToken{},
	&models.SiteInfo{},
	&models.Media{},
	&models.MediaURL{},
	&models.Album{},
	&models.MediaEXIF{},
	&models.VideoMetadata{},
	&models.ShareToken{},
	&models.UserMediaData{},
	&models.UserAlbums{},
	&models.UserPreferences{},

	// Face detection
	&models.FaceGroup{},
	&models.ImageFace{},
}

func MigrateDatabase(db *gorm.DB) error {

	if err := db.SetupJoinTable(&models.User{}, "Albums", &models.UserAlbums{}); err != nil {
		log.Error(nil, "Setup UserAlbums join table failed", "error", err)
	}

	if err := db.AutoMigrate(database_models...); err != nil {
		log.Error(nil, "Auto migration failed", "error", err)
	}

	// v2.1.0 - Replaced by Media.CreatedAt
	if db.Migrator().HasColumn(&models.Media{}, "date_imported") {
		db.Migrator().DropColumn(&models.Media{}, "date_imported")
	}

	// v2.3.0 - Changed type of MediaEXIF.Exposure and MediaEXIF.Flash
	// from string values to decimal and int respectively
	if err := migrateExifFields(db); err != nil {
		log.Error(nil, "Failed to run exif fields migration", "error", err)
	}

	// Remove invalid GPS data from DB
	if err := migrations.MigrateForExifGPSCorrection(db); err != nil {
		log.Error(nil, "Failed to run exif GPS correction migration", "error", err)
	}

	// v2.5.0 - Remove Thumbnail Method for Downsampliing filters
	if db.Migrator().HasColumn(&models.SiteInfo{}, "thumbnail_method") {
		db.Migrator().DropColumn(&models.SiteInfo{}, "thumbnail_method")
	}

	return nil
}

func ClearDatabase(db *gorm.DB) error {
	var errs []error
	for _, model := range database_models {
		if err := db.Migrator().DropTable(model); err != nil {
			errs = append(errs, err)
		}
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("drop tables error: %w", err)
	}

	return nil
}
