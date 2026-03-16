package main

import (
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ─── Cache

type cacheEntry struct {
	data    []byte
	expires time.Time
}

type cache struct {
	mu    sync.RWMutex
	items map[string]cacheEntry
}

func newCache() *cache {
	return &cache{items: make(map[string]cacheEntry)}
}

func (c *cache) get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.items[key]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	return e.data, true
}

func (c *cache) set(key string, data []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = cacheEntry{data: data, expires: time.Now().Add(ttl)}
}

func (c *cache) evictExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.items {
		if now.After(e.expires) {
			delete(c.items, k)
		}
	}
}

// cacheTTL is the maximum time a response is cached (past dates never change).
const cacheTTL = 365 * 24 * time.Hour

// ─── OpenMensa XML generation 

// xmlDay and its children map to the OpenMensa v2 XML schema.
// The root <openmensa> element is written as a raw string to avoid namespace issues.

type xmlDay struct {
	XMLName    xml.Name      `xml:"day"`
	Date       string        `xml:"date,attr"`
	Categories []xmlCategory `xml:"category"`
}

type xmlCategory struct {
	Name  string    `xml:"name,attr"`
	Meals []xmlMeal `xml:"meal"`
}

type xmlMeal struct {
	Name   string     `xml:"name"`
	Notes  []string   `xml:"note"`
	Prices []xmlPrice `xml:"price"`
}

type xmlPrice struct {
	Role  string `xml:"role,attr"`
	Value string `xml:",chardata"`
}

const omProlog = `<?xml version="1.0" encoding="UTF-8"?>
<openmensa version="2.1"
  xmlns="http://openmensa.org/open-mensa-v2"
  xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
  xsi:schemaLocation="http://openmensa.org/open-mensa-v2 http://openmensa.org/open-mensa-v2.xsd">
  <version>1.0</version>`

const omEpilog = `</openmensa>`

var weekdayNames = [7]string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

func metadataXML(info CanteenInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "    <name>%s</name>\n", info.Name)
	fmt.Fprintf(&b, "    <address>%s</address>\n", info.Address)
	fmt.Fprintf(&b, "    <city>%s</city>\n", info.City)
	if info.Phone != "" {
		fmt.Fprintf(&b, "    <phone>%s</phone>\n", info.Phone)
	}
	fmt.Fprintf(&b, "    <location latitude=\"%.4f\" longitude=\"%.4f\"/>\n", info.Latitude, info.Longitude)
	b.WriteString("    <availability>public</availability>\n")
	b.WriteString("    <times type=\"opening\">\n")
	for i, day := range weekdayNames {
		if info.Hours[i] != "" {
			fmt.Fprintf(&b, "      <%s open=\"%s\"/>\n", day, info.Hours[i])
		} else {
			fmt.Fprintf(&b, "      <%s closed=\"true\"/>\n", day)
		}
	}
	b.WriteString("    </times>\n")
	return b.String()
}

func buildXML(canteen, date string, cats []*Category) ([]byte, error) {
	day := xmlDay{Date: date}
	for _, cat := range cats {
		xcat := xmlCategory{Name: cat.Title}
		for _, m := range cat.Meals {
			// Combine allergens and additives into individual notes, mirroring the
			// Python implementation's behaviour of joining them as a single note.
			var notes []string
			combined := append(m.Allergens, m.Additives...)
			if len(combined) > 0 {
				notes = []string{strings.Join(combined, ", ")}
			}
			xcat.Meals = append(xcat.Meals, xmlMeal{
				Name:  m.Title,
				Notes: notes,
				Prices: []xmlPrice{
					{Role: "student", Value: fmt.Sprintf("%.2f", float64(m.StudentPrice)/100)},
					{Role: "employee", Value: fmt.Sprintf("%.2f", float64(m.StaffPrice)/100)},
					{Role: "other", Value: fmt.Sprintf("%.2f", float64(m.GuestPrice)/100)},
				},
			})
		}
		day.Categories = append(day.Categories, xcat)
	}

	dayXML, err := xml.MarshalIndent(day, "    ", "  ")
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString(omProlog)
	buf.WriteString("\n  <canteen>\n")
	if info, ok := canteenInfoMap[canteen]; ok {
		buf.WriteString(metadataXML(info))
	}
	buf.Write(dayXML)
	buf.WriteString("\n  </canteen>\n")
	buf.WriteString(omEpilog)
	return buf.Bytes(), nil
}

// ─── HTTP server ──────────────────────────────────────────────────────────────

type server struct {
	cache    *cache
	fetchMu  sync.Mutex // serialises cache-miss fetches to avoid stampedes
}

func (s *server) getOrFetch(canteen, date string) ([]byte, error) {
	key := canteen + ":" + date
	if data, ok := s.cache.get(key); ok {
		return data, nil
	}
	// Serialize fetches for the same key so we don't hammer the upstream on a cold start.
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()
	if data, ok := s.cache.get(key); ok {
		return data, nil
	}
	return s.fetch(canteen, date)
}

// fetch unconditionally downloads, parses, caches, and returns the XML.
func (s *server) fetch(canteen, date string) ([]byte, error) {
	cats, err := FetchMenu(canteen, date)
	if err != nil {
		return nil, err
	}
	data, err := buildXML(canteen, date, cats)
	if err != nil {
		return nil, err
	}
	s.cache.set(canteen+":"+date, data, cacheTTL)
	return data, nil
}

// refresh fetches fresh data for a canteen+date and updates the cache.
// Used by the background scheduler.
func (s *server) refresh(canteen, date string) {
	cats, err := FetchMenu(canteen, date)
	if err != nil {
		log.Printf("refresh %s/%s: %v", canteen, date, err)
		return
	}
	data, err := buildXML(canteen, date, cats)
	if err != nil {
		log.Printf("buildXML %s/%s: %v", canteen, date, err)
		return
	}
	s.cache.set(canteen+":"+date, data, cacheTTL)
	log.Printf("refreshed %s/%s (%d categories)", canteen, date, len(cats))
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")

	// Route: /                → list canteens
	// Route: /{canteen}       → today's menu
	// Route: /{canteen}/{date}→ menu for specific date
	switch len(parts) {
	case 1:
		if parts[0] == "" {
			s.handleList(w, r)
			return
		}
		s.handleMenu(w, r, parts[0], time.Now().Format("2006-01-02"))
	case 2:
		s.handleMenu(w, r, parts[0], parts[1])
	default:
		http.NotFound(w, r)
	}
}

func (s *server) handleList(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for name := range canteenIDs {
		fmt.Fprintln(w, name)
	}
}

func (s *server) handleMenu(w http.ResponseWriter, r *http.Request, canteen, date string) {
	if _, ok := canteenIDs[canteen]; !ok {
		http.Error(w, fmt.Sprintf("unknown canteen %q\n\nAvailable canteens:", canteen), http.StatusNotFound)
		return
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		http.Error(w, "invalid date format, expected YYYY-MM-DD", http.StatusBadRequest)
		return
	}

	data, err := s.getOrFetch(canteen, date)
	if err != nil {
		log.Printf("ERROR %s %s: %v", r.Method, r.URL, err)
		http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write(data)
}

// ─── Scheduler ────────────────────────────────────────────────────────────────

// nextOccurrence returns the next moment after now when hour:minute occurs.
func nextOccurrence(hour, minute int) time.Time {
	now := time.Now()
	t := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !t.After(now) {
		t = t.Add(24 * time.Hour)
	}
	return t
}

// parseRefreshTimes parses a comma-separated "HH:MM,HH:MM,..." string.
func parseRefreshTimes(s string) [][2]int {
	var times [][2]int
	for _, part := range strings.Split(s, ",") {
		var h, m int
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d:%d", &h, &m); err == nil {
			times = append(times, [2]int{h, m})
		}
	}
	return times
}

func (s *server) runScheduler(refreshTimes [][2]int) {
	if len(refreshTimes) == 0 {
		return
	}
	for {
		// Find the soonest upcoming refresh time.
		var next time.Time
		for _, t := range refreshTimes {
			nt := nextOccurrence(t[0], t[1])
			if next.IsZero() || nt.Before(next) {
				next = nt
			}
		}
		sleep := time.Until(next)
		log.Printf("next scheduled refresh at %s (in %s)", next.Format("15:04"), sleep.Round(time.Second))
		time.Sleep(sleep)

		today := time.Now().Format("2006-01-02")
		log.Printf("running scheduled refresh for %s", today)
		for canteen := range canteenIDs {
			go s.refresh(canteen, today)
		}
	}
}

// ─── main ─────────────────────────────────────────────────────────────────────

func main() {
	port := flag.Int("port", 8080, "TCP port to listen on")
	listen := flag.String("listen", "127.0.0.1", "address to listen on")
	refreshStr := flag.String("refresh", "07:00,11:00,14:00,17:00",
		"comma-separated HH:MM times to refresh today's menu (local time)")
	flag.Parse()

	refreshTimes := parseRefreshTimes(*refreshStr)
	if len(refreshTimes) == 0 {
		log.Fatal("no valid refresh times parsed from --refresh flag")
	}

	srv := &server{cache: newCache()}

	// Background scheduler refreshes today's data at configured times.
	go srv.runScheduler(refreshTimes)

	// Daily cache eviction keeps memory bounded.
	go func() {
		for range time.Tick(24 * time.Hour) {
			srv.cache.evictExpired()
		}
	}()

	addr := fmt.Sprintf("%s:%d", *listen, *port)
	log.Printf("stwb-openmensa listening on http://%s", addr)
	log.Printf("refresh schedule: %s", *refreshStr)
	log.Fatal(http.ListenAndServe(addr, srv))
}
