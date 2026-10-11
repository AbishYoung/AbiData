package demo

// Static content for the Northwind Traders sample workspace. Everything here is
// invented and local; nothing refers to real people or companies.

type customerSeed struct{ Company, Contact, City, Country string }

var customers = []customerSeed{
	{"Alfreds Futterkiste", "Maria Anders", "Berlin", "Germany"},
	{"Ana Trujillo Emparedados", "Ana Trujillo", "Mexico City", "Mexico"},
	{"Around the Horn", "Thomas Hardy", "London", "UK"},
	{"Berglunds snabbkop", "Christina Berglund", "Lulea", "Sweden"},
	{"Blondel pere et fils", "Frederique Citeaux", "Strasbourg", "France"},
	{"Bolido Comidas preparadas", "Martin Sommer", "Madrid", "Spain"},
	{"Bon app'", "Laurence Lebihan", "Marseille", "France"},
	{"Bottom-Dollar Markets", "Elizabeth Lincoln", "Tsawassen", "Canada"},
	{"Cactus Comidas para llevar", "Patricio Simpson", "Buenos Aires", "Argentina"},
	{"Chop-suey Chinese", "Yang Wang", "Bern", "Switzerland"},
	{"Comercio Mineiro", "Pedro Afonso", "Sao Paulo", "Brazil"},
	{"Consolidated Holdings", "Elizabeth Brown", "London", "UK"},
	{"Drachenblut Delikatessen", "Sven Ottlieb", "Aachen", "Germany"},
	{"Du monde entier", "Janine Labrune", "Nantes", "France"},
	{"Eastern Connection", "Ann Devon", "London", "UK"},
	{"Ernst Handel", "Roland Mendel", "Graz", "Austria"},
	{"Familia Arquibaldo", "Aria Cruz", "Sao Paulo", "Brazil"},
	{"FISSA Fabrica Inter. Salchichas", "Diego Roel", "Madrid", "Spain"},
	{"Folies gourmandes", "Martine Rance", "Lille", "France"},
	{"Folk och fa HB", "Maria Larsson", "Bracke", "Sweden"},
	{"Frankenversand", "Peter Franken", "Munich", "Germany"},
	{"Galeria del gastronomo", "Eduardo Saavedra", "Barcelona", "Spain"},
	{"Great Lakes Food Market", "Howard Snyder", "Eugene", "USA"},
	{"Hanari Carnes", "Mario Pontes", "Rio de Janeiro", "Brazil"},
	{"Island Trading", "Helen Bennett", "Cowes", "UK"},
}

type productSeed struct {
	Name, Category string
	Cents          int64 // unit price in cents
}

var products = []productSeed{
	{"Chai", "Beverages", 1800},
	{"Chang", "Beverages", 1900},
	{"Ginger Beer", "Beverages", 1450},
	{"Aniseed Syrup", "Condiments", 1000},
	{"Cajun Seasoning", "Condiments", 2200},
	{"Boysenberry Spread", "Condiments", 2500},
	{"Cranberry Sauce", "Condiments", 4000},
	{"Dried Pears", "Produce", 3000},
	{"Tofu", "Produce", 2325},
	{"Kobe Beef", "Meat/Poultry", 9700},
	{"Smoked Sausage", "Meat/Poultry", 3480},
	{"Salmon Roe", "Seafood", 3100},
	{"Kelp Konbu", "Seafood", 600},
	{"Tiger Prawns", "Seafood", 6250},
	{"Blue Cheese", "Dairy", 2100},
	{"Goat Cheese", "Dairy", 3800},
	{"Pavlova", "Confections", 1745},
	{"Chocolate Biscuits", "Confections", 920},
	{"Hazelnut Spread", "Confections", 1375},
	{"Rye Crispbread", "Grains/Cereals", 900},
}

type employeeSeed struct{ Name, Title, Email string }

var employees = []employeeSeed{
	{"Nancy Davolio", "Sales Representative", "nancy.davolio@northwind.example"},
	{"Andrew Fuller", "Vice President, Sales", "andrew.fuller@northwind.example"},
	{"Janet Leverling", "Sales Representative", "janet.leverling@northwind.example"},
	{"Margaret Peacock", "Sales Representative", "margaret.peacock@northwind.example"},
	{"Steven Buchanan", "Sales Manager", "steven.buchanan@northwind.example"},
	{"Michael Suyama", "Sales Representative", "michael.suyama@northwind.example"},
}
