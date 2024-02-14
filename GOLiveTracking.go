package main

import (
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
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

// Prepared statements as global variables
var (
	stmtWithUserAndSession *sql.Stmt
	stmtWithUserOnly       *sql.Stmt
	stmtGetUserSessions    *sql.Stmt
	stmtInsertPoint        *sql.Stmt
	stmtFetchGpsTrack      *sql.Stmt
)

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
	EventRefreshTime        string `yaml:"EventRefreshTime"`
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
	// Load the application configuration
	ReadConfig()

	// Check if the database exists and create it if it doesn't
	if _, err := os.Stat("./sqlite-database.db"); os.IsNotExist(err) {
		CreateDB()
	}

	// Open a database connection
	db, err := sql.Open("sqlite3", "sqlite-database.db")
	if err != nil {
		checkErr(err)
	}
	defer db.Close() // Ensure the database connection is closed when the main function exits

	// Ping the database to ensure it's ready to accept connections
	if err = db.Ping(); err != nil {
		checkErr(err)
	}

	// Initialize prepared statements
	if err = InitDB(db); err != nil {
		checkErr(err)
	}
	defer CloseDB() // Ensure the prepared statements are closed when the main function exits

	mux := http.NewServeMux()

	mux.HandleFunc("/addpoint", func(w http.ResponseWriter, r *http.Request) { getAddPoint(w, r, db) })
	mux.HandleFunc("/resetpoint", func(w http.ResponseWriter, r *http.Request) { getResetPoint(w, r, db) })
	mux.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) { getResetPointUsrSession(w, r, db) })
	mux.HandleFunc("/download-gpx", func(w http.ResponseWriter, r *http.Request) { getGpxTrack(w, r, db) })
	mux.HandleFunc("/getusersession", func(w http.ResponseWriter, r *http.Request) { getUserSessions(w, r) })
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.Dir("static")))
	mux.Handle("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=604800")
		staticHandler.ServeHTTP(w, r)
	}))
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) { eventsHandler(w, r) })
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

func InitDB(db *sql.DB) error {
	// Initialize the prepared statement when your application starts
	var err error
	stmtWithUserAndSession, err = db.Prepare("SELECT LAT, LON, ALT, SPEED, TIME, BEARING, HDOP, USER, SESSION FROM Points WHERE USER = ? AND SESSION = ? ORDER BY ID DESC LIMIT 1")
	if err != nil {
		return err
	}
	stmtWithUserOnly, err = db.Prepare("SELECT LAT, LON, ALT, SPEED, TIME, BEARING, HDOP, USER, SESSION FROM Points WHERE USER = ? ORDER BY ID DESC LIMIT 1")
	if err != nil {
		stmtWithUserAndSession.Close() // Close the previously prepared statement if the second fails
		return err
	}
	stmtGetUserSessions, err = db.Prepare("SELECT DISTINCT SESSION FROM Points WHERE USER = ?")
	if err != nil {
		return err
	}
	stmtInsertPoint, err = db.Prepare("INSERT INTO Points(LAT, LON, ALT, SPEED, TIME, BEARING, HDOP, USER, SESSION) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	stmtFetchGpsTrack, err = db.Prepare("SELECT LAT, LON, ALT, TIME FROM Points WHERE user = ? AND session = ?")
	if err != nil {
		return err
	}

	return nil
}

func CloseDB() {
	stmtWithUserAndSession.Close()
	stmtWithUserOnly.Close()
	stmtGetUserSessions.Close()
	stmtInsertPoint.Close()
	stmtFetchGpsTrack.Close()
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
		reverseOrder = "ASC"
	}

	var whereClause strings.Builder
	var args []interface{}
	if user != "" {
		whereClause.WriteString(" WHERE user=?")
		args = append(args, user)
		if session != "" {
			whereClause.WriteString(" AND session=?")
			args = append(args, session)
		}
	}

	query := fmt.Sprintf(`
                SELECT lat, lon, alt, speed, time, bearing, hdop
                FROM Points %s
                ORDER BY ID %s
                %s`, whereClause.String(), reverseOrder, limit)

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
		sort.Slice(points, func(i, j int) bool {
			timeI, _ := time.Parse(time.RFC3339, points[i].Time)
			timeJ, _ := time.Parse(time.RFC3339, points[j].Time)
			return timeJ.Before(timeI)
		})
	}

	if err := rows.Err(); err != nil {
		checkErr(err)
	}

	return points
}

func buildLatLonHistory(points []Point) []string {
	// Use strings.Builder for efficient string concatenation
	var latLonHistory strings.Builder

	// Iterate over the points and append each coordinate pair
	for i, point := range points {
		latLonHistory.WriteString(point.Lat + "," + point.Lon)

		// Append a comma separator unless it's the last element in the list
		if i < len(points)-1 {
			latLonHistory.WriteRune('!')
		}
	}

	return strings.Split(latLonHistory.String(), "!")
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

// getUserSessions retrieves user sessions and writes them as links in the response.
func getUserSessions(w http.ResponseWriter, r *http.Request) {
	// Authenticate the request.
	key := r.URL.Query().Get("key")
	if key != AppConfig.Key {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Validate the 'user' parameter.
	user := r.URL.Query().Get("user")
	if !checkParam(user, AppConfig.MaxGetParmLen) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Query the database for sessions associated with the user.
	rows, err := stmtGetUserSessions.Query(user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	// Set the response header and write the session links.
	w.Header().Set("Content-Type", "text/html")
	escapedUser := html.EscapeString(user) // Escape once.
	for _, session := range sessions {
		escapedSession := html.EscapeString(session)
		fmt.Fprintf(w, `<a href="/?user=%s&session=%s">Session %s</a><br><br>`, escapedUser, escapedSession, escapedSession)
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

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Server Error", http.StatusInternalServerError)
		log.Println("Transaction start error:", err)
		return
	}

	// Execute the prepared statement
	_, err = tx.Stmt(stmtInsertPoint).Exec(lat, lon, altitude, speed, timestamp, bearing, hdop, user, session)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Server Error", http.StatusInternalServerError)
		log.Println("Insert exec error:", err)
		return
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		http.Error(w, "Server Error", http.StatusInternalServerError)
		log.Println("Transaction commit error:", err)
		return
	}

	// Send back a successful response
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func sanitize(input string) string {
	return strings.ReplaceAll(input, "\n", "")
}

// eventsHandler serves an HTTP request to stream events.
func eventsHandler(w http.ResponseWriter, r *http.Request) {
	user, session := sanitizeInput(r)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Set refresh interval for the event stream.
	refreshDuration, err := time.ParseDuration(AppConfig.EventRefreshTime)
	if err != nil {
		log.Printf("Invalid refresh duration, using default: %v\n", err)
		refreshDuration = 5 * time.Second // Use a sensible default
	}

	ctx := r.Context()         // Use request context for handling cancellation.
	previousPoint := &LatLng{} // Initialize previous point.

	ticker := time.NewTicker(refreshDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Client has disconnected.
			return
		case <-ticker.C:
			currentPoint, err := getLastKnownPosition(user, session)
			if err != nil {
				log.Printf("Error querying database: %v\n", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			// Send location only if it's new data.
			if currentPoint != nil && !currentPoint.Equal(previousPoint) {
				sendLocationEvent(w, currentPoint)
				*previousPoint = *currentPoint // Update the last sent location.
			}
		}
	}
}

// Equal checks if this point equals another one.
func (a *LatLng) Equal(b *LatLng) bool {
	return a.Lat == b.Lat && a.Lng == b.Lng && a.Alt == b.Alt && a.Speed == b.Speed && a.Time == b.Time && a.Bear == b.Bear && a.Hdop == b.Hdop
}

func sanitizeInput(r *http.Request) (user string, session string) {
	user = r.URL.Query().Get("user")
	session = r.URL.Query().Get("session")
	if user == "null" || user == "" {
		user = "0"
	}
	if session == "null" || session == "" {
		session = "0"
	}
	return user, session
}

// getLastKnownPosition retrieves the last known position for a user and session from the database.
func getLastKnownPosition(user string, session string) (*LatLng, error) {

	if user == "0" {
		return nil, nil
	}

	// Decide which prepared statement to use based on the session value
	var stmt *sql.Stmt
	var args []interface{}
	if session != "0" {
		stmt = stmtWithUserAndSession
		args = []interface{}{user, session}
	} else {
		stmt = stmtWithUserOnly
		args = []interface{}{user}
	}

	// Scan the result into the LatLng struct.
	var point LatLng
	err := stmt.QueryRow(args...).Scan(&point.Lat, &point.Lng, &point.Alt, &point.Speed, &point.Time, &point.Bear, &point.Hdop, &point.User, &point.Session)
	if err != nil {
		return nil, err
	}

	return &point, nil
}

// sendLocationEvent is a function that sends an event containing location information.
func sendLocationEvent(w http.ResponseWriter, point *LatLng) {
	// Marshals (convert into JSON format) lat lng data and checks if there are any errors during the process
	data, err := json.Marshal(point)
	if err != nil { // If error occurs while marshaling...
		log.Printf("Error marshaling JSON: %v\n", err)                         // Log this unexpected event for debugging purpose
		http.Error(w, "Error marshaling JSON", http.StatusInternalServerError) // Return an internal server error to the client
		return
	}
	// Write a response in SSE (Server-Sent Events format):  'event' field set as location and data containing our Marshaled LatLng struct
	fmt.Fprintf(w, "event: location\ndata: %s\n\n", data)

	// Check if the http response writer supports flusher interface (required for Server-Sent Events streaming).  If it does not support this functionality...
	if flusher, ok := w.(http.Flusher); ok { // Attempt to get a reference of Flusher from ResponseWriter
		flusher.Flush() // Flush any buffered data immediately in case we want server send event as soon as possible
	} else {
		log.Println("Streaming unsupported!")
	}
}

func getGpxTrack(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	// Validate key, user, and session parameters
	if err := validateRequestParameters(r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	user := r.URL.Query().Get("user")
	session := r.URL.Query().Get("session")
	if session == "" {
		session = "0"
	}

	// Fetch GPS track data
	points, err := fetchGpsTrack(db, user, session)
	if err != nil {
		http.Error(w, "Query error", http.StatusInternalServerError)
		return
	}

	// Create GPX structure and populate it with track data
	gpx := createGpxStructure("GoLiveTracking", points)

	// Write the GPX file as the HTTP response
	writeGpxResponse(w, gpx)
}

func validateRequestParameters(r *http.Request) error {
	key := r.URL.Query().Get("key")
	if key != AppConfig.Key {
		return fmt.Errorf(http.StatusText(http.StatusUnauthorized))
	}

	user := r.URL.Query().Get("user")
	if user == "" || !isNumeric(user) || len(user) > AppConfig.MaxGetParmLen {
		return fmt.Errorf("Invalid user parameter")
	}

	session := r.URL.Query().Get("session")
	if session != "" && (!isNumeric(session) || len(session) > AppConfig.MaxGetParmLen) {
		return fmt.Errorf("Invalid session parameter")
	}

	return nil
}

func fetchGpsTrack(db *sql.DB, user, session string) ([]GPXPoint, error) {
	var points []GPXPoint

	rows, err := stmtFetchGpsTrack.Query(user, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var p GPXPoint
		if err := rows.Scan(&p.Latitude, &p.Longitude, &p.Elevation, &p.Time); err != nil {
			return nil, err
		}
		points = append(points, p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return points, nil
}

func createGpxStructure(creator string, points []GPXPoint) GPX {
	track := Track{Name: "GPS Track", Segments: []Segment{{Points: points}}}
	return GPX{Version: "1.1", Creator: creator, Tracks: []Track{track}}
}

func writeGpxResponse(w http.ResponseWriter, gpx GPX) {
	w.Header().Set("Content-Disposition", "attachment; filename=my_gps_track.gpx")
	w.Header().Set("Content-Type", "application/gpx+xml")

	enc := xml.NewEncoder(w)
	enc.Indent("", "    ")
	if err := enc.Encode(gpx); err != nil {
		http.Error(w, "Error writing GPX file", http.StatusInternalServerError)
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

// This function converts a given timestamp string into time format based on application configured timezone.
func TimeStampConvert(e string) (dtime time.Time) {
	// Parsing inputted String to Int64, assuming the provided 'string' is in base-10 representation of integer
	data, err := strconv.ParseInt(e, 10, 64)
	if err != nil { // If there were any errors during parsing operation then it prints out error and returns zero value for time.
		fmt.Println(err)
	}
	// Loading location based on the application configured timezone
	loc, err := time.LoadLocation(AppConfig.TimeZone)
	if err != nil { // If there were any errors during loading operation then it prints out error and returns zero value for time.
		fmt.Println(err)
	}
	// Converting the parsed Int64 data into Unix timestamp, dividing by '1000' to convert milliseconds since epoch to seconds because ParseInt function parses till it encounters a non-digit character and returns integer value until that moment which might be in millisecond format.
	dtime = time.Unix(data/1000, 0).In(loc) // 'dtime' variable will hold the converted date based on application configured timezone location after this operation is completed successfully without any errors during parsing or loading operations beforehand .
	return dtime                            // Returning final computed Unix timestamp in specific Timezone.
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
	fmt.Println("Database connection established.")

	// Crea la tabella Points
	_, err = db.Exec(`
        CREATE TABLE Points (
            ID INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
            LAT STRING NOT NULL,
            LON STRING NOT NULL,
            ALT STRING NOT NULL,
            SPEED STRING NOT NULL,
            TIME STRING NOT NULL,
            BEARING STRING NOT NULL,
            HDOP STRING NOT NULL,
            USER STRING NOT NULL,
            SESSION STRING NOT NULL
        );
    `)
	checkErr(err)
	fmt.Println("Table 'Points' created successfully.")

	_, err = db.Exec("CREATE INDEX idx_user ON Points(USER);")
	checkErr(err)
	fmt.Println("Index 'idx_user' created successfully.")

	_, err = db.Exec("CREATE INDEX idx_session ON Points(SESSION);")
	checkErr(err)
	fmt.Println("Index 'idx_session' created successfully.")

	_, err = db.Exec("CREATE INDEX idx_user_session ON Points(USER, SESSION);")
	checkErr(err)
	fmt.Println("Index 'idx_user_session' created successfully.")

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='index';")
	checkErr(err)
	defer rows.Close()

	var indexName string
	for rows.Next() {
		err := rows.Scan(&indexName)
		checkErr(err)
		fmt.Println("Found index:", indexName)
	}
	fmt.Println("Index verification completed.")
}

func checkErr(err error, args ...string) {
	if err != nil {
		fmt.Println("Error")
		fmt.Println(err, " : ", args)
	}
}

// Function that validates if given latitude and longitude values are within the valid range for GPS coordinates.
func isValidCoordinates(lat, lon string) bool {
	// Convert Latitude from String to Float64 type
	latFloat, errLat := strconv.ParseFloat(lat, 64)
	if errLat != nil { // If conversion fails return false indicating invalid latitude value is	invalid so return False
		return false
	}

	// Convert Longitude from string to float64	type
	lonFloat, errLon := strconv.ParseFloat(lon, 64)
	if errLon != nil { // if the conversion is unsuccessful then it indicates that longitude value is invalid so return False
		return false
	}

	// Check and validate	the	input	data	to see	it falls	in	a	valide	gps coordinate range
	return latFloat >= -90 && latFloat <= 90 && lonFloat >= -180 && lonFloat <= 180
}
