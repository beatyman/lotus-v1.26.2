package main

import (
	"flag"
	"log"

	goleveldb "github.com/syndtr/goleveldb/leveldb"
	goleveldbOpt "github.com/syndtr/goleveldb/leveldb/opt"
)

var (
	DB_PATH   string
	DUMP_FILE string
)

func doRepair() {

	opts := &goleveldbOpt.Options{}

	db, err := goleveldb.RecoverFile(DB_PATH, opts)

	if err != nil {
		log.Fatalf("leveldb repair error: %s", err.Error())
	}
	defer db.Close()

	log.Printf("leveldb repair ok.")
}

func main() {

	var dbpath, action *string

	dbpath = flag.String("dbpath", "", "database path")
	action = flag.String("action", "", "action:repair")

	flag.Parse()

	if *dbpath == "" {
		log.Fatal("no database path.")
	}

	if *action != "repair" {
		log.Fatal("invalid action.")
	}

	DB_PATH = *dbpath

	if *action == "repair" {
		doRepair()
	}

}
