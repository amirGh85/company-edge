2. Reproxy را به‌عنوان Data Plane انتخاب کردیم

به‌جای اینکه Reverse Proxy را از صفر بنویسم، از Reproxy v1.7.1 استفاده کردم.

وظیفه Reproxy:

دریافت Request
ارسال Request به Origin
Health Check
تشخیص وضعیت Backendها
Failover بین Primary و Backup


main job : 

                 Normal Traffic
User ───────→ ArvanCloud ───────→ Origin
                    │
                    │ Failure
                    ▼
              Backup Edge
                    │
                    ▼
                  Origin

                  failure : when cdn service is down we can use this backup edge to keep our services up 
                  i use Reproxy instead of craete a new reverse proxy , its take a little time from devoloper 
                  Facilities : 
                  i created two nginx service for monitoring tarfic 


                  Primary Origin
localhost:3000
       │
       └── PRIMARY ORIGIN - company-edge


Backup Origin
localhost:3001
       │
       └── BACKUP ORIGIN - company-edge

       2 : Health check is enabeld for checking situation of backend 


       3 : i sat up failover system for time that  primary : 3000 isnt working . i tested this  and it works .


        in normal situation : 
        Client
  ↓
Reproxy
  ↓
Primary :3000
  ↓
PRIMARY ORIGIN 


in failover time :
Client
  ↓
Reproxy
  ↓
Backup :3001
  ↓
BACKUP ORIGIN

3 : recovery 
when primary origin is fixed the main origin back in sitation 

              ┌──────────────┐
              │ Primary UP   │
              └──────┬───────┘
                     ↓
                  PRIMARY
                     │
                Primary DOWN
                     ↓
                  BACKUP
                     │
                Primary UP
                     ↓
                  PRIMARY

                  sitation of project till now : 
                                   HTTPS
                   │
                   ▼
              ┌─────────┐
              │ Reproxy │
              └────┬────┘
                   │
          ┌────────┴────────┐
          ↓                 ↓
     Primary :3000     Backup :3001
          │                 │
          ↓                 ↓
       Nginx             Nginx