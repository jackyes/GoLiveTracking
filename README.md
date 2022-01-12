AddPoint example:  
http(s)://[address]:[port]/addpoint?lon=11.1475585&lat=44.1247882&timestamp=1639747127&altitude=11&speed=5&bearing=6&user=1&session=1&key=12345 
  
Reset point history:  
http(s)://[address]:[port]/resetpoint?key=12345   
  
Map (example):  
http(s)://[address]:[port]    #All point  
http(s)://[address]:[port]/?user=1&session=1  #Show only User1, session1  
http(s)://[address]:[port]/?user=1&session=1&maxshowpoint=50 #Show only User1, session1 and last 50 rec point (if AllowBypassMaxShowPoint is true in config.yaml)  
  
Adjust config.yaml  
