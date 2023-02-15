package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/yaml.v3"
)

const configPath = "./config.yaml"

var templates = template.Must(template.ParseFiles("pages/index.html"))

type Point struct {
	ID      int
	Lat     string
	Lon     string
	Alt     string
	Speed   string
	Time    string
	Bearing string
	Hdop    string
	User    string
	Session string
}

// Declaration of struct needed for config.yaml
type Cfg struct {
	ServerPort              string `yaml:"ServerPort"`
	ServerPortTLS           string `yaml:"ServerPortTLS"`
	CertPathCrt             string `yaml:"CertPathCrt"`
	CertPathKey             string `yaml:"CertPathKey"`
	Key                     string `yaml:"Key"`
	EnableTLS               bool   `yaml:"EnableTLS"`
	DisableNoTLS            bool   `yaml:"DisableNoTLS"`
	DefaultLat              string `yaml:"DefaultLat"`
	DefaultLon              string `yaml:"DefaultLon"`
	ShowOnlyLastPos         bool   `yaml:"ShowOnlyLastPos"`
	MapRefreshTime          string `yaml:"MapRefreshTime"`
	DefaultZoom             string `yaml:"DefaultZoom"`
	ConsoleDebug            bool   `yaml:"ConsoleDebug"`
	MaxGetParmLen           int    `yaml:"MaxGetParmLen"`
	ShowPrecisonCircle      bool   `yaml:"ShowPrecisonCircle"`
	MinZoom                 string `yaml:"MinZoom"`
	MaxZoom                 string `yaml:"MaxZoom"`
	ConvertTimestamp        bool   `yaml:"ConvertTimestamp"`
	TimeZone                string `yaml:"TimeZone"`
	MaxShowPoint            string `yaml:"MaxShowPoint"`
	ShowMapOnlyWithUser     bool   `yaml:"ShowMapOnlyWithUser"`
	AllowBypassMaxShowPoint bool   `yaml:"AllowBypassMaxShowPoint"`
	EventRefrehTime         string `yaml:"EventRefrehTime"`
}

var AppConfig Cfg
var safeString = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

// need for HTML SSE
type LatLng struct {
	User    string `json:"user"`
	Session string `json:"session"`
	Lat     string `json:"lat"`
	Lng     string `json:"lng"`
	Alt     string `json:"alt"`
	Speed   string `json:"speed"`
	Time    string `json:"time"`
	Bear    string `json:"bear"`
	Hdop    string `json:"hdop"`
}

// Declaration of struct needed for the template
type Page struct {
	Lastpos            string
	Latlonhistory      []string
	DefaultLat         string
	DefaultLon         string
	ShowOnlyLastPos    bool
	MapRefreshTime     string
	DefaultZoom        string
	MinZoom            string
	MaxZoom            string
	ShowPrecisonCircle bool
}

func main() {
	ReadConfig()
	if _, err := os.Stat("./sqlite-database.db"); os.IsNotExist(err) {
		CreateDB()
	}
	db, err := sql.Open("sqlite3", "sqlite-database.db")
	if err != nil {
		checkErr(err)
	}
	defer db.Close()
	if err = db.Ping(); err != nil {
		checkErr(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/addpoint", func(w http.ResponseWriter, r *http.Request) { getAddPoint(w, r, db) })
	mux.HandleFunc("/resetpoint", func(w http.ResponseWriter, r *http.Request) { getResetPoint(w, r, db) })
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) { eventsHandler(w, r, db) })
	mux.HandleFunc("/favicon.ico", faviconHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { IndexHandler(w, r, db) })

	if !AppConfig.DisableNoTLS {
		http.ListenAndServe(":"+AppConfig.ServerPort, mux)
	}
	if AppConfig.EnableTLS {
		err := http.ListenAndServeTLS(":"+AppConfig.ServerPortTLS, AppConfig.CertPathCrt, AppConfig.CertPathKey, mux)
		if err != nil {
			fmt.Println(err)
		}
	}
}

func faviconHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./static/favicon.ico")
}

func IndexHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// Push assets if client supports it
	if pusher, ok := w.(http.Pusher); ok {
		assets := []string{
			"/static/leaflet.css",
			"/static/leaflet.js",
			"/static/images/layers.png",
			"/static/images/marker-icon.png",
			"/static/images/marker-shadow.png",
		}
		for _, asset := range assets {
			if err := pusher.Push(asset, nil); err != nil {
				fmt.Println("Failed to push: ", err)
			}
		}
	}
	// Get query parameters
	user := r.URL.Query().Get("user")
	session := r.URL.Query().Get("session")
	maxshowpoint := r.URL.Query().Get("maxshowpoint")

	if AppConfig.ShowMapOnlyWithUser && user == "" { //show only if user is provided
		http.NotFound(w, r)
		return
	}

	if !checkParam(user, AppConfig.MaxGetParmLen) || !isSafeString(user) {
		return
	}
	if !checkParam(session, AppConfig.MaxGetParmLen) || !isSafeString(session) {
		return
	}
	if !isSafeString(maxshowpoint) || !checkParam(maxshowpoint, AppConfig.MaxGetParmLen) && !AppConfig.AllowBypassMaxShowPoint {
		return
	}

	var latlonhistoryfromDB []string

	var limit string
	if AppConfig.AllowBypassMaxShowPoint && maxshowpoint != "" {
		limit = " LIMIT " + maxshowpoint
	} else if AppConfig.MaxShowPoint != "0" {
		limit = " LIMIT " + AppConfig.MaxShowPoint
	}
	var usrsession string
	if user != "" {
		usrsession = " WHERE user=" + user
		if session != "" {
			usrsession += " AND session=" + session
		}
	}
	type Point struct {
		Lat     string
		Lon     string
		Alt     string
		Speed   string
		Time    string
		Bearing string
		Hdop    string
	}

	query := `
                SELECT lat, lon, alt, speed, time, bearing, hdop
                FROM Points ` + usrsession + `
                ORDER BY ID ASC
                ` + limit

	rows, err := db.Query(query)
	if err != nil {
		checkErr(err)
	}
	defer rows.Close()

	points := make([]Point, 0)

	for rows.Next() {
		var point Point
		if err := rows.Scan(&point.Lat, &point.Lon, &point.Alt, &point.Speed, &point.Time, &point.Bearing, &point.Hdop); err != nil {
			checkErr(err)
		}
		points = append(points, point)
	}

	if err := rows.Err(); err != nil {
		checkErr(err)
	}

	for _, point := range points {
		latlonhistoryfromDB = append(latlonhistoryfromDB, point.Lat+","+point.Lon)
	}

	p := &Page{
		Latlonhistory:      latlonhistoryfromDB,
		DefaultLat:         AppConfig.DefaultLat,
		DefaultLon:         AppConfig.DefaultLon,
		ShowOnlyLastPos:    AppConfig.ShowOnlyLastPos,
		MapRefreshTime:     AppConfig.MapRefreshTime,
		DefaultZoom:        AppConfig.DefaultZoom,
		MinZoom:            AppConfig.MinZoom,
		MaxZoom:            AppConfig.MaxZoom,
		ShowPrecisonCircle: AppConfig.ShowPrecisonCircle,
	}

	renderTemplate(w, "index", p)
}

func checkParam(param string, maxLen int) bool {
	if param != "" && !isNumeric(param) {
		fmt.Println(strings.Replace(param, "\n", "", -1) + " not numeric")
		return false
	} else if len(param) > maxLen {
		fmt.Println(strings.Replace(param, "\n", "", -1) + " too big")
		return false
	}
	return true
}

func getResetPoint(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	key := r.URL.Query().Get("key")
	if key != AppConfig.Key {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	_, err := db.Exec("delete from Points")
	if err != nil {
		checkErr(err)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func getAddPoint(w http.ResponseWriter, r *http.Request, db *sql.DB) {

	lat := r.URL.Query().Get("lat")
	lon := r.URL.Query().Get("lon")
	timestamp := r.URL.Query().Get("timestamp")
	altitude := r.URL.Query().Get("altitude")
	speed := r.URL.Query().Get("speed")
	bearing := r.URL.Query().Get("bearing")
	hdop := r.URL.Query().Get("hdop")
	user := r.URL.Query().Get("user")
	session := r.URL.Query().Get("session")
	key := r.URL.Query().Get("key")

	if key != AppConfig.Key {
		fmt.Println("Wrong key.")
		return
	}

	if AppConfig.ConsoleDebug {
		fmt.Println("lat =>", strings.Replace(lat, "\n", "", -1))
		fmt.Println("lon =>", strings.Replace(lon, "\n", "", -1))
		fmt.Println("timestamp =>", strings.Replace(timestamp, "\n", "", -1))
		fmt.Println("altitude =>", strings.Replace(altitude, "\n", "", -1))
		fmt.Println("speed =>", strings.Replace(speed, "\n", "", -1))
		fmt.Println("bearing =>", strings.Replace(bearing, "\n", "", -1))
		fmt.Println("HDOP =>", strings.Replace(hdop, "\n", "", -1))
		fmt.Println("user =>", strings.Replace(user, "\n", "", -1))
		fmt.Println("session =>", strings.Replace(session, "\n", "", -1))
		fmt.Println("key =>", strings.Replace(key, "\n", "", -1))
	}

	//data verification will happen here...
	if lat == "" || lon == "" {
		fmt.Println("LAT/LON not fund")
		return
	} else if !isNumeric(lat) || !isNumeric(lon) {
		fmt.Println("LAT/LON Not number")
		return
	} else if len(lat) > AppConfig.MaxGetParmLen || len(lon) > AppConfig.MaxGetParmLen {
		fmt.Println("LAT/LON too big")
		return
	}
	if timestamp == "" {
		timestamp = "0"
	} else if !isNumeric(timestamp) {
		fmt.Println("Timestamp not numeric")
		return
	} else if len(timestamp) > AppConfig.MaxGetParmLen {
		fmt.Println("timestamp too big")
		return
	} else if AppConfig.ConvertTimestamp {
		timestamp = fmt.Sprintf("%s", TimeStampConvert(timestamp))
	}
	if altitude == "" {
		altitude = "0"
	} else if !isNumeric(altitude) {
		fmt.Println("Altitude not numeric")
		return
	} else if len(altitude) > AppConfig.MaxGetParmLen {
		fmt.Println("Altitude too big")
		return
	}
	if speed == "" {
		speed = "0"
	} else if !isNumeric(speed) {
		fmt.Println("Speed not numeric")
		return
	} else if len(speed) > AppConfig.MaxGetParmLen {
		fmt.Println("Speed too big")
		return
	}
	if bearing == "" {
		bearing = "0"
	} else if len(bearing) > AppConfig.MaxGetParmLen {
		fmt.Println("bearing too big")
		return
	}
	if hdop == "" {
		hdop = "0"
	} else if !isNumeric(hdop) {
		fmt.Println("hdop not numeric")
		return
	} else if len(hdop) > AppConfig.MaxGetParmLen {
		fmt.Println("hdop too big")
		return
	}
	if user == "" {
		user = "0"
	} else if !isNumeric(user) {
		fmt.Println("User not numeric")
		return
	} else if len(user) > AppConfig.MaxGetParmLen {
		fmt.Println("User too big")
		return
	}
	if session == "" {
		session = "0"
	} else if !isNumeric(session) {
		fmt.Println("Session not numeric")
		return
	} else if len(session) > AppConfig.MaxGetParmLen {
		fmt.Println("Session too big")
		return
	}
	//data verification finish...

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))

	tx, err := db.Begin()
	checkErr(err)
	stmt, err := tx.Prepare("insert into Points(LAT, LON, ALT, SPEED, TIME, BEARING, HDOP, USER, SESSION) values(?, ?, ?, ?, ?, ?, ?, ?, ?)")
	checkErr(err)
	defer stmt.Close()
	_, err = stmt.Exec(lat, lon, altitude, speed, timestamp, bearing, hdop, user, session)
	checkErr(err)
	tx.Commit()
}

func eventsHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	user := r.URL.Query().Get("user")
	session := r.URL.Query().Get("session")
	if user == "null" {
		user = "0"
	} else if !isNumeric(user) {
		fmt.Println("User not numeric")
		return
	} else if len(user) > AppConfig.MaxGetParmLen {
		fmt.Println("User too big")
		return
	}
	if session == "null" {
		session = "0"
	} else if !isNumeric(session) {
		fmt.Println("Session not numeric")
		return
	} else if len(session) > AppConfig.MaxGetParmLen {
		fmt.Println("Session too big")
		return
	}
	// Set the HTTP response header
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	notify := w.(http.CloseNotifier).CloseNotify()

	// Infinite loop to retrieve the last position from the database every X seconds.
	for {
		select {
		case <-notify:
			return
		default:
			where := ""
			if (user != "0") && (session != "0") {
				where = "WHERE USER = " + user + " AND SESSION = " + session
			}
			query := "SELECT * FROM Points " + where + " ORDER BY ID DESC LIMIT 1"
			rows, err := db.Query(query)
			defer rows.Close()

			var point Point
			for rows.Next() {
				// Scan the row and store the last position in the "point" variable
				err := rows.Scan(&point.ID, &point.Lat, &point.Lon, &point.Alt, &point.Speed, &point.Time, &point.Bearing, &point.Hdop, &point.User, &point.Session)
				checkErr(err)
			}
			err = rows.Err()
			checkErr(err)

			// Create a LatLng object with the position data
			location := LatLng{
				User:    point.User,
				Session: point.Session,
				Lat:     point.Lat,
				Lng:     point.Lon,
				Alt:     point.Alt,
				Speed:   point.Speed,
				Time:    point.Time,
				Bear:    point.Bearing,
				Hdop:    point.Hdop,
			}

			// Encode the LatLng object into JSON format
			data, err := json.Marshal(location)
			checkErr(err)

			// Send the "location" event with the position data
			fmt.Fprintf(w, "event: location\ndata: %s\n\n", data)
			w.(http.Flusher).Flush()

			// Wait for AppConfig.EventRefrehTime before retrieving the position from the database again.
			d, err := time.ParseDuration(AppConfig.EventRefrehTime)
			if err != nil {
				fmt.Println("Error parsing EventRefrehTime from config.yaml. Using default value (15s)", err)
				d = 15 * time.Second
			}
			time.Sleep(d)
		}
	}
}

func renderTemplate(w http.ResponseWriter, tmpl string, p *Page) {
	err := templates.ExecuteTemplate(w, tmpl+".html", p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func isNumeric(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func ReadConfig() {
	f, err := os.Open(configPath)
	if err != nil {
		fmt.Println(err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&AppConfig)

	if err != nil {
		fmt.Println(err)
	}
}

func TimeStampConvert(e string) (dtime time.Time) {
	data, err := strconv.ParseInt(e, 10, 64)
	if err != nil {
		fmt.Println(err)
	}
	loc, err := time.LoadLocation(AppConfig.TimeZone)
	if err != nil {
		fmt.Println(err)
	}
	dtime = time.Unix(data/1000, 0).In(loc)
	fmt.Println(dtime)
	return dtime
}

func isSafeString(str string) bool {
	if str == "" {
		return true
	}
	return safeString.MatchString(str)
}

func CreateDB() {
	db, err := sql.Open("sqlite3", "sqlite-database.db")
	checkErr(err)

	// create table
	_, err = db.Exec("create table Points (ID integer NOT NULL PRIMARY KEY AUTOINCREMENT, LAT string not null, LON string not null, ALT string not null, SPEED string not null, TIME string not null, BEARING string not null, HDOP string not null, USER string not null, SESSION string not null); delete from Points;")
	checkErr(err)
}

func checkErr(err error, args ...string) {
	if err != nil {
		fmt.Println("Error")
		fmt.Println(err, " : ", args)
	}
}
