# Very simple Live Gps tracking viewer written in GO.  
Use an app like OsmAnd (or a simple HTTP Get) to send data to the server:  
  
## AddPoint example:  
```
http(s)://[address]:[port]/addpoint?lon=[LON]&lat=[LAT]&timestamp=[UNIXTIMESTAMP]&altitude=[ALT]&speed=[SPEED]&bearing=[BEARING]&user=[USERNR]&session=[SESSIONNR]&key=[KEY]  
```
```
OsmAnd: http(s)://[address]:[port]/addpoint?lat={0}&lon={1}&altitude={4}&acc={3}&timestamp={2}&speed={5}&bearing={6}&user=[USERNR]&session=[SESSIONNR]&key=[Key]
```

  
## Reset point history:  
```
http(s)://[address]:[port]/resetpoint?key=[KEY]
```

  
## Map (example):  
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

## Adjust config.yaml  
