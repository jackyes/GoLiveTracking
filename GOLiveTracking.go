package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"text/template"

	"gopkg.in/yaml.v3"
)

const configPath = "./config.yaml"

var templates = template.Must(template.ParseFiles("pages/index.html"))

// Declaration of struct needed for config.yaml
type Cfg struct {
	ServerPort      string `yaml:"ServerPort"`
	ServerPortTLS   string `yaml:"ServerPortTLS"`
	CertPathCrt     string `yaml:"CertPathCrt"`
	CertPathKey     string `yaml:"CertPathKey"`
	Key             string `yaml:"Key"`
	EnableTLS       bool   `yaml:"EnableTLS"`
	DisableNoTLS    bool   `yaml:"DisableNoTLS"`
	DefaultLat      string `yaml:"DefaultLat"`
	DefaultLon      string `yaml:"DefaultLon"`
	ShowOnlyLastPos bool   `yaml:"ShowOnlyLastPos"`
	MapRefreshTime  string `yaml:"MapRefreshTime"`
	DefaultZoom     string `yaml:"DefaultZoom"`
	ConsoleDebug    bool   `yaml:"ConsoleDebug"`
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
}

func main() {
	ReadConfig()
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

	var latlonhistoryfromfile []string

	PointHistoryfile, err := os.Open("./point.history")
	if err != nil {
		fmt.Println(err)
	}
	defer PointHistoryfile.Close()

	PointHistoryscanner := bufio.NewScanner(PointHistoryfile)
	for PointHistoryscanner.Scan() {
		latlonhistoryfromfile = append(latlonhistoryfromfile, PointHistoryscanner.Text())
	}

	if err := PointHistoryscanner.Err(); err != nil {
		fmt.Println(err)
	}

	contents, err := ioutil.ReadFile("./point.latest")
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	p := &Page{
		Lastpos:         string(contents),      // data from file "./point.latest"
		Latlonhistory:   latlonhistoryfromfile, // data from file "./point.history"
		DefaultLat:      AppConfig.DefaultLat,
		DefaultLon:      AppConfig.DefaultLon,
		ShowOnlyLastPos: AppConfig.ShowOnlyLastPos,
		MapRefreshTime:  AppConfig.MapRefreshTime,
		DefaultZoom:     AppConfig.DefaultZoom,
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
		f := os.Remove("./point.history")
		if f != nil {
			fmt.Println(f)
		}
		history, e := os.Create("./point.history")
		if e != nil {
			fmt.Println(e)
		}
		history.Close()
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
		fmt.Println("key =>", key)
	}

	//data verification will happen here...
	if lat == "" || lon == "" {
		fmt.Println("LAT/LON not fund")
		return
	} else if !isNumeric(lat) || !isNumeric(lon) {
		fmt.Println("LAT/LON Not number")
		return
	} else if len(lat) > 15 || len(lon) > 15 {
		fmt.Println("LAT/LON too big")
		return
	}
	if timestamp == "" {
		timestamp = "0"
	} else if !isNumeric(timestamp) {
		fmt.Println("Timestamp not numeric")
		return
	} else if len(timestamp) > 15 {
		fmt.Println("timestamp too big")
		return
	}
	if altitude == "" {
		altitude = "0"
	} else if !isNumeric(altitude) {
		fmt.Println("Altitude not numeric")
		return
	} else if len(altitude) > 15 {
		fmt.Println("Altitude too big")
		return
	}
	if speed == "" {
		speed = "0"
	} else if !isNumeric(speed) {
		fmt.Println("Speed not numeric")
		return
	} else if len(speed) > 15 {
		fmt.Println("Speed too big")
		return
	}
	if bearing == "" {
		bearing = "0"
	} else if len(bearing) > 15 {
		fmt.Println("bearing too big")
		return
	}
	//data verification finish...

	file, err := os.Create("./point.latest")
	if err != nil {
		fmt.Println("Unable to open file:", err)
	}

	len, err := file.WriteString(fmt.Sprintf("L.marker([%s,%s]).addTo(map).bindPopup('Lat: %s<br>Lon: %s<br>Altitude: %s<br>Speed: %s<br>Time: %s<br>Bearing: %s').openPopup();", lat, lon, lat, lon, altitude, speed, timestamp, bearing))

	if err != nil {
		fmt.Println("Unable to write data:", err)
	}
	fmt.Printf("%d character written successfully into file", len)
	file.Close()

	w.WriteHeader(200)
	w.Write([]byte(lat + "," + lon + ","))

	f, err := os.OpenFile("./point.history", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println(err)
	}
	if _, err := f.Write([]byte(lat + ", " + lon + "\n")); err != nil {
		fmt.Println(err)
	}
	if err := f.Close(); err != nil {
		fmt.Println(err)
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
