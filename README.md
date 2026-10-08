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



## Staging Test — Ubuntu Server

### Environment

- OS: Ubuntu 24.04 LTS
- Reproxy: `v1.7.1`
- Docker: `29.1.3`
- Docker Compose: `2.40.3`
- HTTPS port: `8443`
- Primary Origin: `localhost:3000`
- Backup Origin: `localhost:3001`
- Health Check interval: `5s`

### Test 1 — Reproxy Startup

Reproxy was deployed on the Ubuntu staging server using Docker Compose.

Result:

- Container: `company-reproxy`
- Status: `Up`
- HTTPS port `8443`: listening
- Status: PASS

### Test 2 — Primary Origin

The Primary Origin was deployed as an Nginx Docker container.

Result:

- Container: `test-origin`
- Port: `3000`
- Status: `Up`
- Direct HTTP request succeeded
- Status: PASS

### Test 3 — Backup Origin

The Backup Origin was deployed as an Nginx Docker container.

Result:

- Container: `test-origin-backup`
- Port: `3001`
- Status: `Up`
- Status: PASS

### Test 4 — External HTTPS Access

A temporary MikroTik destination NAT was configured:

`Public :8443 → Ubuntu 10.20.45.252:8443`

The service was tested from an external mobile network.

Result:

- HTTPS endpoint reachable from Internet
- Reproxy responded successfully
- Status: PASS

### Staging Summary

The Company Edge prototype was successfully deployed on the Ubuntu staging server.

Verified:

- Docker deployment
- Reproxy startup
- HTTPS termination
- Primary Origin
- Backup Origin
- External access through MikroTik NAT

Automatic failover/failback was not included in this staging test.
