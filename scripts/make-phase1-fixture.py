import json, os, subprocess, sys, time, urllib.request, http.cookiejar, shutil
S = sys.argv[1]
data = os.path.join(S, "p1data")
shutil.rmtree(data, ignore_errors=True)
os.makedirs(data)
env = dict(os.environ, ABIDATA_DATA_DIR=data, ABIDATA_BACKUP_DIR=os.path.join(data, "backups"),
           ABIDATA_LISTEN_ADDR="127.0.0.1:18095", ABIDATA_PUBLIC_URL="http://127.0.0.1:18095", ABIDATA_LOG_LEVEL="warn")
binp = os.path.join(S, "abidata-p1")
subprocess.run([binp, "user", "create-admin", "--email", "admin@example.com", "--name", "Admin"], input=b"correct horse battery staple\n", env=env, check=True)
srv = subprocess.Popen([binp, "serve"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
try:
    base = "http://127.0.0.1:18095"
    for _ in range(100):
        try:
            urllib.request.urlopen(base + "/healthz"); break
        except Exception:
            time.sleep(0.1)
    jar = http.cookiejar.CookieJar()
    op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    csrf = ""
    def call(method, path, body=None):
        req = urllib.request.Request(base + path, method=method, data=None if body is None else json.dumps(body).encode(),
            headers={"Content-Type": "application/json", "Origin": base, "X-CSRF-Token": csrf})
        try:
            with op.open(req) as r:
                t = r.read()
                return json.loads(t) if t else None
        except urllib.error.HTTPError as e:
            raise SystemExit(f"{method} {path}: {e.code} {e.read().decode()}")
    me = call("POST", "/api/v1/auth/login", {"email": "admin@example.com", "password": "correct horse battery staple"})
    csrf = me["csrf_token"]
    ws = call("POST", "/api/v1/admin/workspaces", {"name": "Phase 1 fixture"})["id"]
    w = f"/api/v1/w/{ws}"
    call("POST", w + "/schema/apply", {"ops": [{"op": "create_table", "table": {"display_name": "Customers", "track_history": True, "columns": [
        {"display_name": "Name", "field_type": "short_text", "is_required": True},
        {"display_name": "Email", "field_type": "short_text"},
        {"display_name": "Notes", "field_type": "long_text"},
        {"display_name": "Visits", "field_type": "integer"},
        {"display_name": "Score", "field_type": "number"},
        {"display_name": "Credit", "field_type": "currency"},
        {"display_name": "Active", "field_type": "yes_no"},
        {"display_name": "Joined", "field_type": "date"},
        {"display_name": "Seen", "field_type": "datetime"},
        {"display_name": "Site", "field_type": "hyperlink"}]}}]})
    sch = call("GET", w + "/schema")
    cust = [t for t in sch["tables"] if t["display_name"] == "Customers"][0]
    col = {c["display_name"]: c["id"] for c in cust["columns"]}
    call("POST", w + "/schema/apply", {"ops": [
        {"op": "create_index", "index": {"table_id": cust["id"], "is_unique": True, "column_ids": [col["Email"]]}},
        {"op": "create_table", "table": {"display_name": "Orders", "columns": [
            {"display_name": "Customer", "field_type": "lookup", "is_required": True, "lookup": {"parent_table_id": cust["id"], "display_column_id": col["Name"]}},
            {"display_name": "Total", "field_type": "currency"}]}}]})
    sch = call("GET", w + "/schema")
    orders = [t for t in sch["tables"] if t["display_name"] == "Orders"][0]
    ocol = {c["display_name"]: c["id"] for c in orders["columns"]}
    ids = []
    for i in range(1, 26):
        r = call("POST", w + f"/tables/{cust['id']}/rows", {"fields": {col["Name"]: f"Customer {i} ✓", col["Email"]: f"c{i}@example.com",
            col["Notes"]: "line1\nline2", col["Visits"]: i, col["Score"]: i / 4, col["Credit"]: f"{i}.25", col["Active"]: i % 2 == 0,
            col["Joined"]: "2026-01-%02d" % i, col["Seen"]: "2026-01-%02dT10:30:00Z" % i, col["Site"]: f"https://example.com/{i}"}})
        ids.append(r["id"])
    for i, cid in enumerate(ids[:10]):
        call("POST", w + f"/tables/{orders['id']}/rows", {"fields": {ocol["Customer"]: cid, ocol["Total"]: f"{i+1}9.99"}})
    print(json.dumps({"workspace": ws, "customers": cust["id"], "orders": orders["id"]}))
finally:
    srv.terminate(); srv.wait()
