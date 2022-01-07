package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"text/template"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/yaml.v3"
)

const configPath = "./config.yaml"

var templates = template.Must(template.ParseFiles("pages/index.html"))

// Declaration of struct needed for config.yaml
type Cfg struct {
	ServerPort          string `yaml:"ServerPort"`
	ServerPortTLS       string `yaml:"ServerPortTLS"`
	CertPathCrt         string `yaml:"CertPathCrt"`
	CertPathKey         string `yaml:"CertPathKey"`
	Key                 string `yaml:"Key"`
	EnableTLS           bool   `yaml:"EnableTLS"`
	DisableNoTLS        bool   `yaml:"DisableNoTLS"`
	DefaultLat          string `yaml:"DefaultLat"`
	DefaultLon          string `yaml:"DefaultLon"`
	ShowOnlyLastPos     bool   `yaml:"ShowOnlyLastPos"`
	MapRefreshTime      string `yaml:"MapRefreshTime"`
	DefaultZoom         string `yaml:"DefaultZoom"`
	ConsoleDebug        bool   `yaml:"ConsoleDebug"`
	MaxGetParmLen       int    `yaml:"MaxGetParmLen"`
	ShowPrecisonCircle  bool   `yaml:"ShowPrecisonCircle"`
	MinZoom             string `yaml:"MinZoom"`
	MaxZoom             string `yaml:"MaxZoom"`
	ConvertTimestamp    bool   `yaml:"ConvertTimestamp"`
	TimeZone            string `yaml:"TimeZone"`
	MaxShowPoint        string `yaml:"MaxShowPoint"`
	ShowMapOnlyWithUser bool   `yaml:"ShowMapOnlyWithUser"`
}

var AppConfig Cfg

// Declaration of struct needed for the template
type Page struct {
	Lastpos         string
	Latlonhistory   []string
	DefaultLat      string
	DefaultLon      string
	ShowOnlyLastPos bool
	MapRefreshTime  string
	DefaultZoom     string
	MinZoom         string
	MaxZoom         string
}

func main() {
	ReadConfig()
	if _, err := os.Stat("./sqlite-database.db"); errors.Is(err, os.ErrNotExist) {
		CreateDB()
	}
	getAddPoint := http.HandlerFunc(getAddPoint)
	http.Handle("/addpoint", getAddPoint)
	getResetPoint := http.HandlerFunc(getResetPoint)
	http.Handle("/resetpoint", getResetPoint)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", http.HandlerFunc(IndexHandler))
	if !AppConfig.DisableNoTLS {
		http.ListenAndServe(":"+AppConfig.ServerPort, nil)
	}
	if AppConfig.EnableTLS {
		err := http.ListenAndServeTLS(":"+AppConfig.ServerPortTLS, AppConfig.CertPathCrt, AppConfig.CertPathKey, nil)
		fmt.Println(err)
	}
}

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	user := r.URL.Query().Get("user")
	session := r.URL.Query().Get("session")
	if AppConfig.ShowMapOnlyWithUser == true && user == "" { //show only if user is provided
		http.NotFound(w, r)
		return
	}

	if user != "" && !isNumeric(user) {
		fmt.Println("User not numeric")
		return
	} else if len(user) > AppConfig.MaxGetParmLen {
		fmt.Println("User too big")
		return
	}
	if session != "" && !isNumeric(session) {
		fmt.Println("Session not numeric")
		return
	} else if len(session) > AppConfig.MaxGetParmLen {
		fmt.Println("Session too big")
		return
	}

	var latlonhistoryfromDB []string

	db, err := sql.Open("sqlite3", "sqlite-database.db")
	checkErr(err)
	defer db.Close()
	checkErr(db.Ping())
	var limit string = ""
	if AppConfig.MaxShowPoint != "0" {
		limit = " LIMIT " + AppConfig.MaxShowPoint
	}
	var usrsession string = ""
	if user != "" {
		usrsession = " WHERE user=" + user
		if session != "" {
			usrsession = " WHERE user=" + user + " AND session=" + session
		}
	}
	rows, err := db.Query("SELECT lat, lon FROM Points " + usrsession + " ORDER BY ID DESC" + limit)
	checkErr(err)
	defer rows.Close()

	//5.1 Iterate through result set
	for rows.Next() {
		var latDB string
		var lonDB string
		err := rows.Scan(&latDB, &lonDB)
		checkErr(err)
		latlonhistoryfromDB = append(latlonhistoryfromDB, latDB+" ,"+lonDB)
	}

	//5.2 check error, if any, that were encountered during iteration
	err = rows.Err()
	checkErr(err)

	contents, err := ioutil.ReadFile("./point.latest")
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	p := &Page{
		Lastpos:         string(contents),    // data from file "./point.latest"
		Latlonhistory:   latlonhistoryfromDB, // data from DB
		DefaultLat:      AppConfig.DefaultLat,
		DefaultLon:      AppConfig.DefaultLon,
		ShowOnlyLastPos: AppConfig.ShowOnlyLastPos,
		MapRefreshTime:  AppConfig.MapRefreshTime,
		DefaultZoom:     AppConfig.DefaultZoom,
		MinZoom:         AppConfig.MinZoom,
		MaxZoom:         AppConfig.MaxZoom,
	}
	renderTemplate(w, "index", p)
}

func getResetPoint(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == AppConfig.Key {
		e := os.Remove("./point.latest")
		if e != nil {
			fmt.Println(e)
		}
		latest, e := os.Create("./point.latest")
		if e != nil {
			fmt.Println(e)
		}
		latest.Close()
		f := os.Remove("./sqlite-database.db")
		if f != nil {
			fmt.Println(f)
		}
		CreateDB()
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	}
}

func getAddPoint(w http.ResponseWriter, r *http.Request) {

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
		fmt.Println("lat =>", lat)
		fmt.Println("lon =>", lon)
		fmt.Println("timestamp =>", timestamp)
		fmt.Println("altitude =>", altitude)
		fmt.Println("speed =>", speed)
		fmt.Println("bearing =>", bearing)
		fmt.Println("HDOP =>", hdop)
		fmt.Println("user =>", user)
		fmt.Println("session =>", session)
		fmt.Println("key =>", key)
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

	file, err := os.Create("./point.latest")
	if err != nil {
		fmt.Println("Unable to open file:", err)
	}

	len, err := file.WriteString(fmt.Sprintf("L.marker([%s,%s]).addTo(map).bindPopup('Lat: %s<br>Lon: %s<br>Altitude: %s<br>Speed: %s<br>Time: %s<br>Bearing: %s<br>HDOP: %s').openPopup();", lat, lon, lat, lon, altitude, speed, timestamp, bearing, hdop))
	if AppConfig.ShowPrecisonCircle {
		len, err := file.WriteString(fmt.Sprintf(`
var circle = L.circle([%s, %s], {
	color: 'blue',
	fillColor: '#g03',
	fillOpacity: 0.3,
	radius: %s
}).addTo(map);`, lat, lon, hdop))
		if AppConfig.ConsoleDebug {
			fmt.Printf("%d character written successfully into file (Precision).\n", len)
		}
		if err != nil {
			fmt.Println("Unable to write data:", err)
		}
	}
	if err != nil {
		fmt.Println("Unable to write data:", err)
	}
	if AppConfig.ConsoleDebug {
		fmt.Printf("%d character written successfully into file.\n", len)
	}
	file.Close()

	w.WriteHeader(200)
	w.Write([]byte(lat + "," + lon + ","))

	db, err := sql.Open("sqlite3", "sqlite-database.db")
	checkErr(err)
	defer db.Close()
	checkErr(db.Ping())
	tx, err := db.Begin()
	checkErr(err)
	stmt, err := tx.Prepare("insert into Points(LAT, LON, ALT, SPEED, TIME, BEARING, HDOP, USER, SESSION) values(?, ?, ?, ?, ?, ?, ?, ?, ?)")
	checkErr(err)
	defer stmt.Close()
	_, err = stmt.Exec(lat, lon, altitude, speed, timestamp, bearing, hdop, user, session)
	checkErr(err)
	tx.Commit()
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
	loc, _ := time.LoadLocation(AppConfig.TimeZone)
	dtime = time.Unix(data/1000, 0).In(loc)
	fmt.Println(dtime)
	return
}
func CreateDB() {
	db, err := sql.Open("sqlite3", "sqlite-database.db")
	checkErr(err)
	defer db.Close()
	checkErr(db.Ping())

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
