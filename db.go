package gosms

import (
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"os"
)

var db *sql.DB

func InitDB(driver, dbname string) (*sql.DB, error) {
	var err error

	if _, err := os.Stat(dbname); os.IsNotExist(err) {
		log.Printf("InitDB: database does not exist %s, creating", dbname)
	}

	if db, err = sql.Open(driver, dbname); err != nil {
		return nil, err
	}

	if err = updateDB(); err != nil {
		return nil, err
	}

	return db, nil
}

func updateDB() (err error) {

	res1, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name='messages'");
	if err != nil {
		return err
	}

	defer res1.Close();
	if res1.Next() == false {
		//create messages table
		createMessages := `CREATE TABLE messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
			uuid char(32) UNIQUE NOT NULL,
			message char(160)   NOT NULL,
			mobile   char(15)    NOT NULL,
			status  INTEGER DEFAULT 0,
			retries INTEGER DEFAULT 0,
			device string NULL,
			created_at TIMESTAMP default CURRENT_TIMESTAMP,
			updated_at TIMESTAMP
		    );`
		if _, err = db.Exec(createMessages, nil); err != nil {
			return err
		}
	}


	res2, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name='incoming'");
	if err != nil {
		return err
	}

	defer res2.Close();
	if res2.Next() == false {
		createIncoming := `CREATE TABLE incoming (
			id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
			message char(160)   NOT NULL,
			mobile   char(15)    NOT NULL,
			device string NULL,
			created_at TIMESTAMP default CURRENT_TIMESTAMP
		    );`
		if _, err = db.Exec(createIncoming, nil); err != nil {
			return err
		}
	}

	return nil
}

func insertOutgoingMessage(sms *OutgoingSMS) error {
	_, err := db.Exec("INSERT INTO messages(uuid, message, mobile, created_at) VALUES(?, ?, ?, DATETIME('now'))", sms.UUID, sms.Body, sms.Mobile)
	return err
}

func updateOutgoingMessageStatus(sms OutgoingSMS) error {
	_, err := db.Exec("UPDATE messages SET status=?, retries=?, device=?, updated_at=DATETIME('now') WHERE uuid=?", sms.Status, sms.Retries, sms.Device, sms.UUID)
	return err
}

func getPendingOutgoingMessages(bufferSize int) ([]OutgoingSMS, error) {
	query := fmt.Sprintf("SELECT uuid, message, mobile, status, retries FROM messages WHERE status!=%v AND retries<%v LIMIT %v", SMSProcessed, SMSRetryLimit, bufferSize)

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []OutgoingSMS

	for rows.Next() {
		sms := OutgoingSMS{}
		rows.Scan(&sms.UUID, &sms.Body, &sms.Mobile, &sms.Status, &sms.Retries)
		messages = append(messages, sms)
	}
	rows.Close()
	return messages, nil
}

func GetOutgoingMessages(filter string) ([]OutgoingSMS, error) {
	/*
	   expecting filter as empty string or WHERE clauses,
	   simply append it to the query to get desired set out of database
	*/
	query := fmt.Sprintf("SELECT id, uuid, message, mobile, status, retries, device, created_at, updated_at FROM messages %v", filter)

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []OutgoingSMS

	for rows.Next() {
		sms := OutgoingSMS{}
		rows.Scan(&sms.Id, &sms.UUID, &sms.Body, &sms.Mobile, &sms.Status, &sms.Retries, &sms.Device, &sms.CreatedAt, &sms.UpdatedAt)
		messages = append(messages, sms)
	}
	rows.Close()
	return messages, nil
}

func GetLast7DaysMessageCount() (map[string]int, error) {

	rows, err := db.Query(`SELECT strftime('%Y-%m-%d', created_at) as datestamp,
    COUNT(id) as messagecount FROM messages GROUP BY datestamp
    ORDER BY datestamp DESC LIMIT 7`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dayCount := make(map[string]int)
	var day string
	var count int
	for rows.Next() {
		rows.Scan(&day, &count)
		dayCount[day] = count
	}
	rows.Close()
	return dayCount, nil
}

func GetStatusSummary() ([]int, error) {
	rows, err := db.Query(`SELECT status, COUNT(id) as messagecount
    FROM messages GROUP BY status ORDER BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var status, count int
	statusSummary := make([]int, 3)
	for rows.Next() {
		rows.Scan(&status, &count)
		statusSummary[status] = count
	}
	rows.Close()
	return statusSummary, nil
}


func insertIncomingMessage(sms *IncomingSMS) error {
	_, err := db.Exec("INSERT INTO incoming(message, mobile, device, created_at) VALUES(?, ?, ?, DATETIME('now'))", sms.Body, sms.Mobile, sms.Device)
	return err
}

func GetIncomingMessages(filter string) ([]IncomingSMS, error) {
	/*
	   expecting filter as empty string or WHERE clauses,
	   simply append it to the query to get desired set out of database
	*/
	query := fmt.Sprintf("SELECT id, message, mobile, device, created_at FROM incoming %v", filter)

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []IncomingSMS

	for rows.Next() {
		sms := IncomingSMS{}
		rows.Scan(&sms.Id, &sms.Body, &sms.Mobile, &sms.Device, &sms.CreatedAt)
		messages = append(messages, sms)
	}
	rows.Close()
	return messages, nil
}