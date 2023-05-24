package main

import (
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/NYTimes/gziphandler"

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

type GPX struct {
	XMLName xml.Name `xml:"gpx"`
	Version string   `xml:"version,attr"`
	Creator string   `xml:"creator,attr"`
	Tracks  []Track  `xml:"trk"`
}

type Track struct {
	Name     string    `xml:"name"`
	Segments []Segment `xml:"trkseg"`
}

type Segment struct {
	Points []GPXPoint `xml:"trkpt"`
}

type GPXPoint struct {
	Latitude  float64 `xml:"lat,attr"`
	Longitude float64 `xml:"lon,attr"`
	Elevation float64 `xml:"ele"`
	Time      string  `xml:"time"`
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
	mux.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) { getResetPointUsrSession(w, r, db) })
	mux.HandleFunc("/download-gpx", func(w http.ResponseWriter, r *http.Request) { getGpxTrack(w, r, db) })
	mux.HandleFunc("/getusersession", func(w http.ResponseWriter, r *http.Request) { getUserSessions(w, r, db) })
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.Dir("static")))
	mux.Handle("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=604800")
		staticHandler.ServeHTTP(w, r)
	}))
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) { eventsHandler(w, r, db) })
	mux.HandleFunc("/favicon.ico", faviconHandler)
	mux.Handle("/", gziphandler.GzipHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { IndexHandler(w, r, db) })))

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
	pushAssets(w)
	// Get query parameters
	user := r.URL.Query().Get("user")
	session := r.URL.Query().Get("session")
	maxshowpoint := r.URL.Query().Get("maxshowpoint")

	if AppConfig.ShowMapOnlyWithUser && user == "" { //show only if user is provided
		http.NotFound(w, r)
		return
	}

	if !isValidParam(user, AppConfig.MaxGetParmLen) || !isValidParam(session, AppConfig.MaxGetParmLen) || (!AppConfig.AllowBypassMaxShowPoint && !isValidParam(maxshowpoint, AppConfig.MaxGetParmLen)) {
		return
	}

	points := fetchPointsFromDB(db, user, session, maxshowpoint)
	latlonhistoryfromDB := buildLatLonHistory(points)

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

func isValidParam(param string, maxLen int) bool {
	return checkParam(param, maxLen) && isSafeString(param)
}

func pushAssets(w http.ResponseWriter) {
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
}

func fetchPointsFromDB(db *sql.DB, user, session, maxShowPoint string) []Point {
	var limit string
	var reverseOrder string = "ASC"
	if AppConfig.AllowBypassMaxShowPoint && maxShowPoint != "" {
		limit = " LIMIT ?"
		reverseOrder = "DESC"
	} else if AppConfig.MaxShowPoint != "0" {
		limit = " LIMIT ?"
		reverseOrder = "DESC"
	}

	var whereClause string
	var args []interface{}
	if user != "" {
		whereClause = " WHERE user=?"
		args = append(args, user)
		if session != "" {
			whereClause += " AND session=?"
			args = append(args, session)
		}
	}

	query := fmt.Sprintf(`
                SELECT lat, lon, alt, speed, time, bearing, hdop
                FROM Points %s
                ORDER BY ID %s
                %s`, whereClause, reverseOrder, limit)

	stmt, err := db.Prepare(query)
	if err != nil {
		checkErr(err)
	}
	defer stmt.Close()

	if limit != "" {
		if maxShowPoint != "" {
			args = append(args, maxShowPoint)
		} else {
			args = append(args, AppConfig.MaxShowPoint)
		}
	}

	rows, err := stmt.Query(args...)
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
	if reverseOrder == "DESC" {
		for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
			points[i], points[j] = points[j], points[i]
		}
	}

	if err := rows.Err(); err != nil {
		checkErr(err)
	}

	return points
}

func buildLatLonHistory(points []Point) []string {
	latlonhistoryfromDB := make([]string, 0, len(points))
	var builder strings.Builder
	for _, point := range points {
		builder.Reset()
		builder.WriteString(point.Lat)
		builder.WriteByte(',')
		builder.WriteString(point.Lon)
		latlonhistoryfromDB = append(latlonhistoryfromDB, builder.String())
	}

	return latlonhistoryfromDB
}

func checkParam(param string, maxLen int) bool {
	if param != "" {
		if !isNumeric(param) {
			fmt.Printf("%s not numeric\n", sanitize(param))
			return false
		} else if len(param) > maxLen {
			fmt.Printf("%s too big\n", sanitize(param))
			return false
		}
	}
	return true
}

func getResetPoint(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	key := r.URL.Query().Get("key")
	if key != AppConfig.Key {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	_, err := db.Exec("delete from Points")
	if err != nil {
		checkErr(err)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func getResetPointUsrSession(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	user := r.URL.Query().Get("user")
	session := r.URL.Query().Get("session")
	key := r.URL.Query().Get("key")

	if key != AppConfig.Key {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	stmt, err := db.Prepare("delete from Points where USER=? and SESSION=?")
	if err != nil {
		checkErr(err)
	}
	_, err = stmt.Exec(user, session)
	if err != nil {
		checkErr(err)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func getUserSessions(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	key := r.URL.Query().Get("key")
	if key != AppConfig.Key {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	user := r.URL.Query().Get("user")
	if !checkParam(user, AppConfig.MaxGetParmLen) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("SELECT DISTINCT SESSION FROM Points WHERE USER = ?")
	if err != nil {
		checkErr(err)
		return
	}
	defer stmt.Close()

	rows, err := stmt.Query(user)
	if err != nil {
		checkErr(err)
		return
	}
	defer rows.Close()

	var sessions []string
	for rows.Next() {
		var session string
		if err := rows.Scan(&session); err != nil {
			checkErr(err)
			return
		}
		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		checkErr(err)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	for _, session := range sessions {
		fmt.Fprintf(w, `<a href="/?user=%s&session=%s">Session %s</a><br><br>`, user, session, session)
	}
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
		fmt.Printf("lat => %s\nlon => %s\ntimestamp => %s\naltitude => %s\nspeed => %s\nbearing => %s\nHDOP => %s\nuser => %s\nsession => %s\nkey => %s\n",
			sanitize(lat), sanitize(lon), sanitize(timestamp), sanitize(altitude), sanitize(speed), sanitize(bearing), sanitize(hdop), sanitize(user), sanitize(session), sanitize(key))
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
	if !isValidCoordinates(lat, lon) {
		fmt.Println("Invalid coordinates")
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

func sanitize(input string) string {
	return strings.ReplaceAll(input, "\n", "")
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
			var query string
			if (user != "0") && (session != "0") {
				query = "SELECT * FROM Points WHERE USER = ? AND SESSION = ? ORDER BY ID DESC LIMIT 1"
			} else if (user != "0") && (session == "0") {
				query = "SELECT * FROM Points WHERE USER = ? ORDER BY ID DESC LIMIT 1"
			} else {
				query = "SELECT * FROM Points ORDER BY ID DESC LIMIT 1"
			}

			var point Point
			var err error

			switch {
			case (user != "0") && (session != "0"):
				err = db.QueryRow(query, user, session).Scan(&point.ID, &point.Lat, &point.Lon, &point.Alt, &point.Speed, &point.Time, &point.Bearing, &point.Hdop, &point.User, &point.Session)
			case (user != "0"):
				err = db.QueryRow(query, user).Scan(&point.ID, &point.Lat, &point.Lon, &point.Alt, &point.Speed, &point.Time, &point.Bearing, &point.Hdop, &point.User, &point.Session)
			default:
				err = db.QueryRow(query).Scan(&point.ID, &point.Lat, &point.Lon, &point.Alt, &point.Speed, &point.Time, &point.Bearing, &point.Hdop, &point.User, &point.Session)
			}

			if err != nil {
				if err == sql.ErrNoRows {
					// no such user/session exists
					continue
				} else {
					http.Error(w, "Database error", http.StatusInternalServerError)
					return
				}
			}

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

func getGpxTrack(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	// Extraction of user and session parameters from the query string
	key := r.URL.Query().Get("key")
	if key != AppConfig.Key {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	user := r.URL.Query().Get("user")
	if user == "" || !isNumeric(user) || len(user) > AppConfig.MaxGetParmLen {
		http.Error(w, "Invalid user parameter", http.StatusBadRequest)
		return
	}
	session := r.URL.Query().Get("session")
	if session == "" {
		session = "0"
	} else if !isNumeric(session) || len(session) > AppConfig.MaxGetParmLen {
		http.Error(w, "Invalid session parameter", http.StatusBadRequest)
		return
	}

	// Query to select the GPS track of the specified user and session
	query := `
                SELECT LAT, LON, ALT, TIME
                FROM Points
                WHERE user = ? AND session = ?
                ORDER BY TIME ASC
        `
	rows, err := db.Query(query, user, session)
	if err != nil {
		http.Error(w, "Query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Creation of the GPX structure
	gpx := GPX{
		Version: "1.1",
		Creator: "GoLiveTracking",
		Tracks: []Track{{
			Name: "GPS Track",
		}},
	}

	// Populating the GPX structure with the GPS track data
	var lat, lon, ele float64
	var t string
	var segment Segment
	for rows.Next() {
		err = rows.Scan(&lat, &lon, &ele, &t)
		if err != nil {
			http.Error(w, "Error reading from db", http.StatusInternalServerError)
			return
		}

		point := GPXPoint{
			Latitude:  lat,
			Longitude: lon,
			Elevation: ele,
			Time:      t,
		}

		segment.Points = append(segment.Points, point)

	}
	// Adding the last segment to the current track
	gpx.Tracks[0].Segments = append(gpx.Tracks[0].Segments, segment)

	// Setting the HTTP headers for downloading the GPX file
	w.Header().Set("Content-Disposition", "attachment; filename=my_gps_track.gpx")
	w.Header().Set("Content-Type", "application/gpx+xml")

	// Writing the HTTP response as a GPX file
	enc := xml.NewEncoder(w)
	enc.Indent("", "    ")
	err = enc.Encode(gpx)
	if err != nil {
		http.Error(w, "Error writing GPX file", http.StatusInternalServerError)
		return
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

func isValidCoordinates(lat, lon string) bool {
	latFloat, errLat := strconv.ParseFloat(lat, 64)
	lonFloat, errLon := strconv.ParseFloat(lon, 64)

	return latFloat >= -90 && latFloat <= 90 && lonFloat >= -180 && lonFloat <= 180 && errLat == nil && errLon == nil
}
