package mysql

import (
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql" // Blank import because the library says to.
	"github.com/jinzhu/gorm"
)

// DBconn is a struct for maintaining a connection to the MySQL Database
type DBconn struct {
	DB *gorm.DB
}

// Initialize attempts to open and verify a connection to the DB
func (d *DBconn) Connect(address string) error {
	var err error

	log.Printf("IM AN INIT FUNC\n\n\n")

	for i := 0; i < 18; i++ {
		d.DB, err = gorm.Open("mysql", address)
		if err != nil {
			log.Printf("Could not connect. Sleeping for 10s %s\n", err)
			time.Sleep(10 * time.Second)
			continue
		}

		err = d.DB.DB().Ping()
		if err != nil {
			log.Printf("Could not Ping. Sleeping for 10s %s\n", err)
			time.Sleep(10 * time.Second)
			continue
		}

		if err == nil {
			return nil
		}
	}

	return fmt.Errorf("Could not connect to database: %s\n", err)
}
