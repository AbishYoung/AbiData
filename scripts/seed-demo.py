#!/usr/bin/env python3
"""Seeds a demo workspace ("Northwind Traders") through the public API.

Usage:
  ABIDATA_PASSWORD=... scripts/seed-demo.py --url http://localhost:8080 --email admin@example.com
  ABIDATA_PASSWORD=... scripts/seed-demo.py --url ... --email ... --setup-token TOKEN   # first run

The password is read from ABIDATA_PASSWORD so it stays out of shell history.
With --setup-token the platform administrator is created first. Standard
library only; https with a self-signed certificate needs --insecure.
"""

import argparse
import http.cookiejar
import json
import os
import random
import ssl
import sys
import urllib.error
import urllib.request


class Client:
    def __init__(self, base, insecure):
        self.base = base.rstrip("/")
        ctx = ssl.create_default_context()
        if insecure:
            ctx.check_hostname = False
            ctx.verify_mode = ssl.CERT_NONE
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
            urllib.request.HTTPSHandler(context=ctx),
        )
        self.csrf = ""

    def call(self, method, path, body=None):
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(self.base + path, data=data, method=method)
        req.add_header("Origin", self.base)
        req.add_header("Content-Type", "application/json")
        if self.csrf:
            req.add_header("X-CSRF-Token", self.csrf)
        try:
            with self.opener.open(req) as resp:
                raw = resp.read()
        except urllib.error.HTTPError as e:
            sys.exit(f"{method} {path}: HTTP {e.code}: {e.read().decode(errors='replace')}")
        out = json.loads(raw) if raw else {}
        if isinstance(out, dict) and out.get("csrf_token"):
            self.csrf = out["csrf_token"]
        return out


def table(name, columns):
    return {"op": "create_table", "table": {"display_name": name, "track_history": True, "columns": columns}}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--url", required=True)
    ap.add_argument("--email", required=True)
    ap.add_argument("--setup-token")
    ap.add_argument("--workspace", default="Northwind Traders")
    ap.add_argument("--insecure", action="store_true")
    a = ap.parse_args()
    password = os.environ.get("ABIDATA_PASSWORD") or sys.exit("set ABIDATA_PASSWORD")
    c = Client(a.url, a.insecure)
    rnd = random.Random(7)

    if a.setup_token:
        c.call("POST", "/api/v1/auth/setup", {"token": a.setup_token, "display_name": "Admin", "password": password})
    else:
        c.call("POST", "/api/v1/auth/login", {"email": a.email, "password": password})
    c.call("GET", "/api/v1/auth/me")
    ws = c.call("POST", "/api/v1/admin/workspaces", {"name": a.workspace, "owner_email": a.email})["id"]
    w = f"/api/v1/w/{ws}"

    sch = c.call("POST", f"{w}/schema/apply", {"ops": [
        table("Customers", [
            {"display_name": "Company", "field_type": "short_text", "is_required": True},
            {"display_name": "Contact", "field_type": "short_text"},
            {"display_name": "City", "field_type": "short_text"},
            {"display_name": "Country", "field_type": "short_text"},
            {"display_name": "Email", "field_type": "short_text"},
            {"display_name": "Active", "field_type": "yes_no"},
        ]),
        table("Products", [
            {"display_name": "Product", "field_type": "short_text", "is_required": True},
            {"display_name": "Category", "field_type": "short_text"},
            {"display_name": "Unit price", "field_type": "currency"},
            {"display_name": "In stock", "field_type": "integer"},
            {"display_name": "Discontinued", "field_type": "yes_no"},
        ]),
    ]})["schema"]
    tables = {t["display_name"]: t for t in sch["tables"]}
    cols = lambda t: {col["display_name"]: col["id"] for col in tables[t]["columns"]}
    cust, prod = cols("Customers"), cols("Products")

    sch = c.call("POST", f"{w}/schema/apply", {"ops": [table("Orders", [
        {"display_name": "Customer", "field_type": "lookup",
         "lookup": {"parent_table_id": tables["Customers"]["id"], "display_column_id": cust["Company"]}},
        {"display_name": "Order date", "field_type": "date"},
        {"display_name": "Status", "field_type": "short_text"},
        {"display_name": "Total", "field_type": "currency"},
        {"display_name": "Notes", "field_type": "long_text"},
    ])]})["schema"]
    tables = {t["display_name"]: t for t in sch["tables"]}
    order = cols("Orders")

    companies = [
        ("Alfreds Futterkiste", "Maria Anders", "Berlin", "Germany"),
        ("Ana Trujillo Emparedados", "Ana Trujillo", "México D.F.", "Mexico"),
        ("Around the Horn", "Thomas Hardy", "London", "UK"),
        ("Berglunds snabbköp", "Christina Berglund", "Luleå", "Sweden"),
        ("Blondel père et fils", "Frédérique Citeaux", "Strasbourg", "France"),
        ("Bólido Comidas preparadas", "Martín Sommer", "Madrid", "Spain"),
        ("Bon app'", "Laurence Lebihan", "Marseille", "France"),
        ("Bottom-Dollar Markets", "Elizabeth Lincoln", "Tsawassen", "Canada"),
        ("Cactus Comidas para llevar", "Patricio Simpson", "Buenos Aires", "Argentina"),
        ("Chop-suey Chinese", "Yang Wang", "Bern", "Switzerland"),
        ("Comércio Mineiro", "Pedro Afonso", "São Paulo", "Brazil"),
        ("Consolidated Holdings", "Elizabeth Brown", "London", "UK"),
        ("Drachenblut Delikatessen", "Sven Ottlieb", "Aachen", "Germany"),
        ("Du monde entier", "Janine Labrune", "Nantes", "France"),
        ("Eastern Connection", "Ann Devon", "London", "UK"),
        ("Ernst Handel", "Roland Mendel", "Graz", "Austria"),
    ]
    cust_ids = []
    for name, contact, city, country in companies:
        r = c.call("POST", f"{w}/tables/{tables['Customers']['id']}/rows", {"fields": {
            cust["Company"]: name, cust["Contact"]: contact, cust["City"]: city, cust["Country"]: country,
            cust["Email"]: contact.split()[0].lower() + "@" + name.split()[0].lower().strip("'") + ".example",
            cust["Active"]: rnd.random() > 0.15}})
        cust_ids.append(r["id"])

    products = [
        ("Chai", "Beverages", "18.00"), ("Chang", "Beverages", "19.00"), ("Aniseed Syrup", "Condiments", "10.00"),
        ("Chef Anton's Cajun Seasoning", "Condiments", "22.00"), ("Grandma's Boysenberry Spread", "Condiments", "25.00"),
        ("Uncle Bob's Organic Dried Pears", "Produce", "30.00"), ("Northwoods Cranberry Sauce", "Condiments", "40.00"),
        ("Mishi Kobe Niku", "Meat/Poultry", "97.00"), ("Ikura", "Seafood", "31.00"), ("Queso Cabrales", "Dairy", "21.00"),
        ("Konbu", "Seafood", "6.00"), ("Tofu", "Produce", "23.25"), ("Pavlova", "Confections", "17.45"),
        ("Carnarvon Tigers", "Seafood", "62.50"), ("Teatime Chocolate Biscuits", "Confections", "9.20"),
    ]
    for name, cat, price in products:
        c.call("POST", f"{w}/tables/{tables['Products']['id']}/rows", {"fields": {
            prod["Product"]: name, prod["Category"]: cat, prod["Unit price"]: price,
            prod["In stock"]: rnd.randint(0, 120), prod["Discontinued"]: rnd.random() < 0.1}})

    statuses = ["New", "Packed", "Shipped", "Shipped", "Delivered", "Delivered", "Delivered"]
    for i in range(40):
        c.call("POST", f"{w}/tables/{tables['Orders']['id']}/rows", {"fields": {
            order["Customer"]: rnd.choice(cust_ids),
            order["Order date"]: f"2026-{rnd.randint(6, 10):02d}-{rnd.randint(1, 28):02d}",
            order["Status"]: rnd.choice(statuses),
            order["Total"]: f"{rnd.randint(40, 2400)}.{rnd.randint(0, 99):02d}",
            order["Notes"]: "" if rnd.random() < 0.7 else "Customer asked for delivery before noon."}})

    o = tables["Orders"]["physical_name"]
    cu = tables["Customers"]["physical_name"]
    phys = {col["display_name"]: col["physical_name"] for col in tables["Orders"]["columns"]}
    cphys = {col["display_name"]: col["physical_name"] for col in tables["Customers"]["columns"]}
    c.call("POST", f"{w}/queries", {
        "name": "Revenue by country", "kind": "sql", "description": "Delivered order totals per customer country.",
        "parameters": [],
        "sql_text": f'SELECT c."{cphys["Country"]}" AS Country, count(*) AS Orders, sum(o."{phys["Total"]}") / 10000.0 AS Revenue '
                    f'FROM "{o}" o JOIN "{cu}" c ON c.id = o."{phys["Customer"]}" '
                    f'WHERE o."{phys["Status"]}" = \'Delivered\' GROUP BY 1 ORDER BY Revenue DESC'})
    c.call("POST", f"{w}/queries", {
        "name": "Open orders", "kind": "sql", "description": "Orders that have not been delivered.",
        "parameters": [],
        "sql_text": f'SELECT o.id, c."{cphys["Company"]}" AS Customer, o."{phys["Order date"]}" AS "Order date", '
                    f'o."{phys["Status"]}" AS Status FROM "{o}" o JOIN "{cu}" c ON c.id = o."{phys["Customer"]}" '
                    f'WHERE o."{phys["Status"]}" <> \'Delivered\' ORDER BY 3'})
    sc = c.call("POST", f"{w}/scripts", {"name": "Nightly summary", "kind": "job", "source":
        f'local n = db.query_value(\'SELECT count(*) FROM "{o}" WHERE "{phys["Status"]}" <> \\\'Delivered\\\'\')\n'
        'log.info("open orders", {count = n})\n'
        'artifacts.save("summary.csv", csv.encode({{"open_orders", n}}))\n'})
    c.call("POST", f"{w}/jobs", {"name": "Nightly summary", "script_id": sc["id"], "schedule": "0 2 * * *"})
    print(f"seeded workspace {a.workspace} ({ws})")


if __name__ == "__main__":
    main()
