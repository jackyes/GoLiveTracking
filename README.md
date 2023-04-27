# GoLiveTracking.  
This is a web-based GPS tracking application that allows users to track GPS coordinates of vehicles or devices on a map in real-time. It stores location data in a SQLite database and serves the web pages and data through a Go web server.  

## Prerequisites
To run this application, you will need Go installed on your machine.

## Installation
Clone this repository: 
```
git clone https://github.com/jackyes/GoLiveTracking.git
```
Navigate to the cloned directory and install dependencies: 
```
go mod tidy.
```
Customize config.yaml as necessary.
Build and run the program:
```
go run main.go
```  
    
## Docker  

It is possible to use an image on Docker Hub with the following command:

    docker run -p 8080:8080 --name golivetracking -v /home/user/config.yaml:/config.yaml jackyes/golivetracking 
    
`/home/user/config.yaml` is the path to your config.yaml file (copy and edit the one in this repository).  
change the default port 8080 accordingly with the one in config.yaml if you modify it.
  
### Build Docker image yourself  
It is possible to create a Docker container following these steps:  
Clone the repository  

    git clone https://github.com/jackyes/GoLivetracking.git  
    
Edit the config.yaml file  
  
    cd GoLiveTracking
    nano config.yaml
  
Create the Docker container  
  
    docker build -t golivetracking .  
  
Run the container  
  
    docker run -p 8080:8080 golivetracking  
  
  
## Usage
Once the application is running, navigate to the web interface by visiting http://localhost:8080 in your web browser.

Use an app like OsmAnd (or a simple HTTP Get) to send data to the server:  
  
## Adding GPS coordinates:  
You can add GPS coordinates to the map by sending a GET request to the /addpoint endpoint with the following parameters:  
```
user: the name of the user associated with the device.
session: the session ID of the device.
lat: the latitude of the GPS coordinates.
lon: the longitude of the GPS coordinates.
alt: the altitude of the GPS coordinates.
speed: the speed of the device.
time: the timestamp of the GPS coordinates.
bearing: the bearing of the device.
hdop: the horizontal dilution of precision of the GPS signal.
```
  
Example:
```
http(s)://[address]:[port]/addpoint?lon=[LON]&lat=[LAT]&timestamp=[UNIXTIMESTAMP]&altitude=[ALT]&speed=[SPEED]&bearing=[BEARING]&user=[USERNR]&session=[SESSIONNR]&key=[KEY]  
```
[OsmAnd](https://osmand.net/):
```
http(s)://[address]:[port]/addpoint?lat={0}&lon={1}&altitude={4}&acc={3}&timestamp={2}&speed={5}&bearing={6}&user=[USERNR]&session=[SESSIONNR]&key=[Key]
```
[GPS Logger](https://f-droid.org/it/packages/com.mendhak.gpslogger/):
```
http(s)://[address]:[port]/addpoint?lat=%LAT&lon=%LON&timestamp=%TIMESTAMP&speed=%SPD&altitude=%ALT&hdop=%HDOP&user=[USERNR]5&session=[SESSIONNR]&key=[Key]
```
## Resetting the map
You can reset the map and remove all GPS coordinates by sending a GET request to the /resetpoint endpoint.
```
http(s)://[address]:[port]/resetpoint?key=[KEY]
```  

## Get a GPX file
```
http(s)://[address]:[port]/download-gpx?user=[UsrNr]&session=[SessionNr]&key=[KEY]
```  

## Server-Sent Events (SSE)
The application uses HTML5 Server-Sent Events (SSE) to push location updates to the client in real-time. The /events endpoint returns a stream of JSON-encoded location updates.

## Map (example):  
The web interface displays a map with the latest GPS coordinates for all devices in the database. You can customize the map and data settings by modifying the config.yaml file.  
### Show all points:
```
http(s)://[address]:[port]
```   
### Show only User1, session1   
```
http(s)://[address]:[port]/?user=1&session=1
```   
### Show only User1, session1 and last 50 rec point (if AllowBypassMaxShowPoint is true in config.yaml)
```  
http(s)://[address]:[port]/?user=1&session=1&maxshowpoint=50   
```  


