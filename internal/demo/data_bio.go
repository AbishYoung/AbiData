package demo

// Static content for the Biosecurity Watch sample workspace: ADR 0010
// (geographic fields) and ADR 0011 (attachments). Farms, people and places
// are invented; the coordinates are the real cities'.

type supplierSeed struct {
	Name, Contact, City, Country string
	Lat, Lon                     float64
}

// suppliers are the farms in the regional biosecurity programme. The first four
// have sightings; the last two are clean.
var suppliers = []supplierSeed{
	{"Kentish Orchards Ltd", "Harriet Cole", "Maidstone", "UK", 51.2704, 0.5227},
	{"Loire Valley Growers", "Etienne Marchand", "Tours", "France", 47.3941, 0.6848},
	{"Lehigh Valley Berry Farm", "Dana Whitfield", "Allentown", "USA", 40.6084, -75.4902},
	{"Black Forest Fruit Co-op", "Jonas Keller", "Freiburg", "Germany", 47.9990, 7.8421},
	{"Po Plain Produce", "Chiara Bellini", "Parma", "Italy", 44.8015, 10.3279},
	{"Cascade Hop Yards", "Marcus Lindqvist", "Salem", "USA", 44.9429, -123.0351},
}

type speciesSeed struct{ Name, Scientific, Category, Risk, Notes string }

var speciesList = []speciesSeed{
	{"Japanese knotweed", "Reynoutria japonica", "Plant", "High", "Spreads from rhizome fragments; never mow or compost cuttings. Treat with licensed herbicide."},
	{"Asian hornet", "Vespa velutina", "Insect", "High", "Preys on honey bees. Report nests; do not disturb them. Professional removal only."},
	{"Spotted lanternfly", "Lycorma delicatula", "Insect", "High", "Feeds on vines and fruit trees. Check vehicles and pallets for egg masses."},
	{"Brown marmorated stink bug", "Halyomorpha halys", "Insect", "Medium", "Damages fruit and vegetables; adults shelter in buildings in autumn."},
	{"Box tree moth", "Cydalima perspectalis", "Insect", "Medium", "Caterpillars strip box hedges. Pheromone traps help to time treatment."},
	{"Giant hogweed", "Heracleum mantegazzianum", "Plant", "High", "The sap causes severe skin burns in sunlight. Wear protective clothing."},
}

// cluster is a group of sightings around one farm.
type cluster struct {
	Supplier int     // index in suppliers
	N        int     // sightings
	RadiusM  float64 // sightings are scattered within this distance of the farm
	MinAge   int     // days before the seed time
	MaxAge   int
	Species  []int // indexes in speciesList, repeated for weight
	Count    int   // maximum abundance
	Cleaned  bool  // all statuses Treated or Cleared (the zone was lifted)
}

var clusters = []cluster{
	{Supplier: 0, N: 14, RadiusM: 3700, MinAge: 2, MaxAge: 120, Species: []int{0, 0, 0, 0, 4, 4, 1, 5}, Count: 40},
	{Supplier: 1, N: 12, RadiusM: 2900, MinAge: 1, MaxAge: 90, Species: []int{1, 1, 1, 1, 1, 0, 3}, Count: 6},
	{Supplier: 2, N: 9, RadiusM: 2700, MinAge: 100, MaxAge: 176, Species: []int{2, 2, 2, 2, 3}, Count: 25, Cleaned: true},
	{Supplier: 3, N: 5, RadiusM: 1500, MinAge: 18, MaxAge: 150, Species: []int{3, 3, 4, 5}, Count: 12},
}

// zoneSeed is a quarantine zone: a ring of (bearing, distance) vertices around
// a supplier's farm, optionally with one circular hole.
type zoneSeed struct {
	Name     string
	Supplier int
	Species  int
	Declared int    // days before the seed time
	Status   string // Active or Lifted
	Notes    string
	Ring     [][2]float64 // bearing in degrees clockwise from north, distance in metres
	Hole     *[3]float64  // east offset, north offset, radius (metres), relative to the farm
}

var zones = []zoneSeed{
	{Name: "Medway Knotweed Containment", Supplier: 0, Species: 0, Declared: 95, Status: "Active",
		Notes: "No movement of soil or green waste out of the zone. Treatment crews report weekly.",
		Ring:  [][2]float64{{0, 2600}, {40, 3100}, {85, 2700}, {130, 3300}, {180, 2500}, {225, 3000}, {270, 2600}, {315, 3100}}},
	{Name: "Loire Hornet Exclusion", Supplier: 1, Species: 1, Declared: 60, Status: "Active",
		Notes: "Nest searches within 2 km. The orchard block inside the zone is treated separately and is excluded.",
		Ring:  [][2]float64{{0, 2300}, {50, 2600}, {100, 2200}, {150, 2700}, {200, 2300}, {250, 2600}, {300, 2200}},
		Hole:  &[3]float64{400, -300, 700}},
	{Name: "Lehigh Lanternfly Zone", Supplier: 2, Species: 2, Declared: 170, Status: "Lifted",
		Notes: "Lifted after two clear seasons of trapping. Kept for the record.",
		Ring:  [][2]float64{{0, 1800}, {72, 2100}, {144, 1900}, {216, 2200}, {288, 1800}}},
}

var sightingNotes = []string{
	"", "", "", "",
	"Found along the hedge line next to the packing shed.",
	"Photographed from the tractor road; not touched.",
	"Second visit: the patch has grown since the last report.",
	"Reported by a seasonal picker at the end of the shift.",
	"Seen again after the rain; local council informed.",
	"On the boundary with the neighbouring farm.",
}
