package main

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// canteenIDs maps canteen slugs to the IDs used by studierendenwerk-bonn.de.
var canteenIDs = map[string]string{
	"SanktAugustin":      "1",
	"CAMPO":              "2",
	"Hofgarten":          "3",
	"FoodtruckRheinbach": "5",
	"VenusbergBistro":    "6",
	"CasinoZEFZEI":       "8",
	"Foodtruck":          "19",
	"Rabinstrasse":       "21",
	"Rheinbach":          "22",
}

// CanteenInfo holds static metadata for a canteen.
type CanteenInfo struct {
	Name      string
	Address   string
	City      string
	Phone     string
	Latitude  float64
	Longitude float64
	// Hours contains opening hours for Mon–Sun (index 0=Mon … 6=Sun).
	// An empty string means closed that day.
	Hours [7]string
}

// canteenInfoMap holds the static metadata for each canteen.
var canteenInfoMap = map[string]CanteenInfo{
	"SanktAugustin": {
		Name:      "Mensa Sankt Augustin",
		Address:   "Grantham-Allee 20, 53757 Sankt Augustin",
		City:      "Sankt Augustin",
		Phone:     "+49 228 73-7131",
		Latitude:  50.7740,
		Longitude: 7.1880,
		Hours:     [7]string{"11:30-14:00", "11:30-14:00", "11:30-14:00", "11:30-14:00", "11:30-14:00", "", ""},
	},
	"CAMPO": {
		Name:      "Mensa CAMPO",
		Address:   "Endenicher Allee 19, 53115 Bonn",
		City:      "Bonn",
		Phone:     "+49 228 73-7131",
		Latitude:  50.7287,
		Longitude: 7.0855,
		Hours:     [7]string{"11:30-14:30", "11:30-14:30", "11:30-14:30", "11:30-14:30", "11:30-14:00", "", ""},
	},
	"Hofgarten": {
		Name:      "Mensa am Hofgarten",
		Address:   "Regina-Pacis-Weg 3, 53113 Bonn",
		City:      "Bonn",
		Phone:     "+49 228 73-7131",
		Latitude:  50.7361,
		Longitude: 7.1018,
		Hours:     [7]string{"11:30-14:30", "11:30-14:30", "11:30-14:30", "11:30-14:30", "11:30-14:30", "", ""},
	},
	"FoodtruckRheinbach": {
		Name:      "Foodtruck Rheinbach",
		Address:   "Von-Liebig-Straße 20, 53359 Rheinbach",
		City:      "Rheinbach",
		Phone:     "+49 228 73-7131",
		Latitude:  50.6244,
		Longitude: 6.9497,
		Hours:     [7]string{"11:30-14:00", "11:30-14:00", "11:30-14:00", "11:30-14:00", "11:30-14:00", "", ""},
	},
	"VenusbergBistro": {
		Name:      "venusberg bistro",
		Address:   "Venusberg-Campus 1, 53127 Bonn",
		City:      "Bonn",
		Phone:     "+49 228 73-7131",
		Latitude:  50.7013,
		Longitude: 7.1218,
		Hours:     [7]string{"08:30-15:00", "08:30-15:00", "08:30-15:00", "08:30-15:00", "08:30-15:00", "08:30-15:00", ""},
	},
	"CasinoZEFZEI": {
		Name:      "Casino ZEF/ZEI",
		Address:   "Genscherallee 3, 53113 Bonn",
		City:      "Bonn",
		Phone:     "+49 228 73-7131",
		Latitude:  50.7270,
		Longitude: 7.0971,
		Hours:     [7]string{"12:00-15:00", "12:00-15:00", "12:00-15:00", "12:00-15:00", "12:00-15:00", "", ""},
	},
	"Foodtruck": {
		Name:      "Foodtruck",
		Address:   "Endenicher Allee 19, 53115 Bonn",
		City:      "Bonn",
		Phone:     "+49 228 73-7131",
		Latitude:  50.7287,
		Longitude: 7.0855,
		Hours:     [7]string{"11:30-14:30", "11:30-14:30", "11:30-14:30", "11:30-14:30", "11:30-14:00", "", ""},
	},
	"Rabinstrasse": {
		Name:      "Leah's World Cafe",
		Address:   "Rabinstraße 8, 53111 Bonn",
		City:      "Bonn",
		Phone:     "+49 228 73-7131",
		Latitude:  50.7260,
		Longitude: 7.0982,
		Hours:     [7]string{"08:00-16:00", "08:00-16:00", "08:00-16:00", "08:00-16:00", "08:00-16:00", "", ""},
	},
	"Rheinbach": {
		Name:      "Mensa Rheinbach",
		Address:   "Von-Liebig-Straße 20, 53359 Rheinbach",
		City:      "Rheinbach",
		Phone:     "+49 228 73-7131",
		Latitude:  50.6244,
		Longitude: 6.9497,
		Hours:     [7]string{"11:30-14:00", "11:30-14:00", "11:30-14:00", "11:30-14:00", "11:30-14:00", "", ""},
	},
}

const mensaURL = "https://www.studierendenwerk-bonn.de/?type=1732731666"

// Meal holds a single menu item and its metadata.
type Meal struct {
	Title        string
	Allergens    []string
	Additives    []string
	StudentPrice int // euro cents
	StaffPrice   int // euro cents
	GuestPrice   int // euro cents
}

// Category holds a named group of meals.
type Category struct {
	Title string
	Meals []*Meal
}

// FetchMenu downloads and parses the menu for a given canteen and date.
// date must be in YYYY-MM-DD format.
func FetchMenu(canteen, date string) ([]*Category, error) {
	id, ok := canteenIDs[canteen]
	if !ok {
		return nil, fmt.Errorf("unknown canteen %q", canteen)
	}
	resp, err := http.PostForm(mensaURL, url.Values{
		"tx_festwb_mealsajax[date]":     {date},
		"tx_festwb_mealsajax[canteen]":  {id},
		"tx_festwb_mealsajax[language]": {"0"}, // German
	})
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return parseMenuHTML(string(body))
}

// tokenRe matches either an HTML tag or a text node.
// Group 1: closing slash; Group 2: tag name; Group 3: raw attrs; Group 4: text content.
var tokenRe = regexp.MustCompile(`(?s)<(/?)\s*(\w[\w-]*)([^>]*)>|([^<]+)`)

// allowedTags are the only tags the parser responds to when they have no attributes.
var allowedTags = map[string]bool{
	"h2": true, "h3": true, "h5": true,
	"strong": true, "p": true, "th": true, "td": true, "br": true,
}

// Parser mode constants mirror the Python state machine.
const (
	mInit     = "INIT"
	mIgnore   = "IGNORE"
	mNewCat   = "NEW_CAT"
	mNewMeal  = "NEW_MEAL"
	mCO2      = "CO2"
	mInfos    = "NEW_INFOS"
	mAllergen = "ALLERGENS"
	mAdditive = "ADDITIVES"
	mPriceCat = "PRICE_CAT"
	mPriceStu = "PRICE_STU"
	mPriceSta = "PRICE_STA"
	mPriceGue = "PRICE_GUE"
)

type mensaParser struct {
	cats     []*Category
	currCat  *Category
	currMeal *Meal
	mode     string
}

// flushCat appends the current meal (if any) to the current category,
// then appends the category to the result list.
func (p *mensaParser) flushCat() {
	if p.currCat == nil {
		return
	}
	if p.currMeal != nil {
		p.currCat.Meals = append(p.currCat.Meals, p.currMeal)
		p.currMeal = nil
	}
	p.cats = append(p.cats, p.currCat)
	p.currCat = nil
}

// flushMeal appends the current meal to the current category.
func (p *mensaParser) flushMeal() {
	if p.currMeal == nil {
		return
	}
	if p.currCat == nil {
		p.currCat = &Category{Title: "Sonstiges"}
	}
	p.currCat.Meals = append(p.currCat.Meals, p.currMeal)
	p.currMeal = nil
}

// tag processes an opening HTML tag (no closing slash, no attributes for relevant tags).
func (p *mensaParser) tag(tag, rawAttrs string) {
	// Self-closing tags like <br/> have a trailing slash in rawAttrs; strip it.
	attrs := strings.TrimRight(strings.TrimSpace(rawAttrs), "/")
	if attrs != "" || !allowedTags[tag] {
		p.mode = mIgnore
		return
	}
	switch tag {
	case "h2":
		p.flushCat()
		p.mode = mNewCat
	case "h3":
		p.mode = mCO2
	case "h5":
		p.flushMeal()
		p.mode = mNewMeal
	case "strong":
		p.mode = mInfos
	case "p":
		if p.currCat == nil && p.currMeal == nil {
			p.mode = mIgnore
		}
		// else: keep current mode so allergens after a <p> still parse
	case "th":
		p.mode = mPriceCat
	// br, td: no mode change
	}
}

var priceDigitRe = regexp.MustCompile(`\d`)

// parsePrice extracts all digits from a price string and returns euro cents.
// e.g. "2,50 €" → 250
func parsePrice(s string) int {
	n := 0
	for _, d := range priceDigitRe.FindAllString(s, -1) {
		n = n*10 + int(d[0]-'0')
	}
	return n
}

// data processes a text node, respecting the current parser mode.
func (p *mensaParser) data(raw string) {
	if p.mode == mIgnore {
		return
	}
	s := html.UnescapeString(strings.TrimSpace(raw))
	if s == "" {
		return
	}
	switch p.mode {
	case mInit:
		// header text before any category — skip
	case mNewCat:
		p.currCat = &Category{Title: s}
	case mNewMeal:
		p.currMeal = &Meal{Title: s}
	case mCO2:
		// ignore CO₂ emission numbers
	case mInfos:
		switch s {
		case "Allergene":
			p.mode = mAllergen
		case "Zusatzstoffe":
			p.mode = mAdditive
		default:
			// CO₂ quality tag strings or other info — ignore
			p.mode = mIgnore
		}
	case mAllergen:
		if p.currMeal != nil {
			p.currMeal.Allergens = append(p.currMeal.Allergens, s)
		}
	case mAdditive:
		if p.currMeal != nil {
			p.currMeal.Additives = append(p.currMeal.Additives, s)
		}
	case mPriceCat:
		switch s {
		case "Stud.":
			p.mode = mPriceStu
		case "Bed.":
			p.mode = mPriceSta
		case "Gast":
			p.mode = mPriceGue
		}
	case mPriceStu:
		if p.currMeal != nil {
			p.currMeal.StudentPrice = parsePrice(s)
		}
	case mPriceSta:
		if p.currMeal != nil {
			p.currMeal.StaffPrice = parsePrice(s)
		}
	case mPriceGue:
		if p.currMeal != nil {
			p.currMeal.GuestPrice = parsePrice(s)
		}
	}
}

// parseMenuHTML tokenises the mensa HTML and runs the state-machine parser.
func parseMenuHTML(htmlStr string) ([]*Category, error) {
	p := &mensaParser{mode: mInit}
	for _, m := range tokenRe.FindAllStringSubmatch(htmlStr, -1) {
		closing, tag, attrs, text := m[1], strings.ToLower(m[2]), m[3], m[4]
		if text != "" {
			p.data(text)
		} else if closing == "" && tag != "" {
			p.tag(tag, attrs)
		}
		// end tags are intentionally ignored (same as Python implementation)
	}
	p.flushCat()
	return p.cats, nil
}
