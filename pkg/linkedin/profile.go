package linkedin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/guidebee/jobseeker/pkg/browser"
)

// Profile holds data scraped from a LinkedIn public profile page.
type Profile struct {
	Name           string          `json:"name"`
	Headline       string          `json:"headline"`
	About          string          `json:"about"`
	Location       string          `json:"location"`
	URL            string          `json:"url"`
	Experience     []Experience    `json:"experience"`
	Education      []Education     `json:"education"`
	Certifications []Certification `json:"certifications"`
	Projects       []Project       `json:"projects"`
	Languages      []Language      `json:"languages"`
	Skills         []string        `json:"skills"`
}

// Experience represents a single work experience entry.
type Experience struct {
	Title    string `json:"title"`
	Company  string `json:"company"`
	Duration string `json:"duration"`
	Location string `json:"location"`
	Desc     string `json:"desc"`
}

// Education represents a single education entry.
type Education struct {
	School string `json:"school"`
	Degree string `json:"degree"`
	Field  string `json:"field"`
	Years  string `json:"years"`
}

// Certification represents a professional certification.
type Certification struct {
	Name   string `json:"name"`
	Issuer string `json:"issuer"`
	Date   string `json:"date"`
}

// Project represents a project entry.
type Project struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// Language represents a language proficiency entry.
type Language struct {
	Name        string `json:"name"`
	Proficiency string `json:"proficiency"`
}

// jsonLDPerson is a minimal schema.org/Person JSON-LD structure.
type jsonLDPerson struct {
	Type        string `json:"@type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	JobTitle    string `json:"jobTitle"`
	URL         string `json:"url"`
	Address     struct {
		AddressLocality string `json:"addressLocality"`
		AddressRegion   string `json:"addressRegion"`
		AddressCountry  string `json:"addressCountry"`
	} `json:"address"`
	WorksFor struct {
		Name string `json:"name"`
	} `json:"worksFor"`
	AlumniOf []struct {
		Name string `json:"name"`
	} `json:"alumniOf"`
}

var (
	jsonLDRe   = regexp.MustCompile(`(?s)<script[^>]+type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	showMoreRe = regexp.MustCompile(`(?i)\s*(show\s+more|show\s+less)\s*`)
)

// FetchProfile fetches and parses a LinkedIn public profile.
// profileURL should be of the form https://www.linkedin.com/in/<username>/
// Pass a non-nil pool to route through the stealth browser (recommended);
// pass nil to fall back to a direct HTTP request.
func FetchProfile(profileURL string, pool *browser.Pool) (*Profile, error) {
	var body string
	var err error
	if pool != nil {
		body, err = pool.FetchLinkedInHTML(profileURL)
	} else {
		body, err = fetchPage(profileURL)
	}
	if err != nil {
		return nil, err
	}

	profile := &Profile{URL: profileURL}

	// 1. Try to extract structured JSON-LD data first (most reliable).
	if err := extractJSONLD(body, profile); err != nil {
		_ = err // non-fatal
	}

	// 2. Supplement with HTML parsing.
	if err := extractHTML(body, profile); err != nil {
		_ = err // non-fatal
	}

	if profile.Name == "" {
		return nil, fmt.Errorf("could not extract profile data from %s (LinkedIn may have blocked the request)", profileURL)
	}

	return profile, nil
}

// fetchPage is the direct HTTP fallback used when no browser pool is available.
func fetchPage(url string) (string, error) {
	body, err := doFetch(url)
	if err == nil {
		return body, nil
	}
	msg := err.Error()
	if strings.Contains(msg, "status 999") {
		return "", fmt.Errorf("%w\nHint: Chrome is not available; LinkedIn blocked the direct request", err)
	}
	if strings.Contains(msg, "status 404") || strings.Contains(msg, "status 503") || strings.Contains(msg, "status 429") {
		fmt.Println("  Got transient error, retrying in 15s...")
		time.Sleep(15 * time.Second)
		return doFetch(url)
	}
	return "", err
}

func doFetch(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Full set of headers that Chrome 120 sends for a direct navigation.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Referer", "https://www.google.com/")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("sec-ch-ua", `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch profile: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("LinkedIn returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return string(data), nil
}

// extractJSONLD pulls schema.org/Person data from JSON-LD script blocks.
func extractJSONLD(body string, p *Profile) error {
	matches := jsonLDRe.FindAllStringSubmatch(body, -1)
	for _, m := range matches {
		raw := strings.TrimSpace(m[1])

		var person jsonLDPerson
		if err := json.Unmarshal([]byte(raw), &person); err == nil && person.Type == "Person" {
			applyJSONLDPerson(&person, p)
			return nil
		}

		var items []json.RawMessage
		if err := json.Unmarshal([]byte(raw), &items); err == nil {
			for _, item := range items {
				var person jsonLDPerson
				if err := json.Unmarshal(item, &person); err == nil && person.Type == "Person" {
					applyJSONLDPerson(&person, p)
					return nil
				}
			}
		}
	}
	return fmt.Errorf("no JSON-LD Person found")
}

func applyJSONLDPerson(person *jsonLDPerson, p *Profile) {
	if person.Name != "" && p.Name == "" {
		p.Name = person.Name
	}
	if person.JobTitle != "" && p.Headline == "" {
		p.Headline = person.JobTitle
	}
	if person.Description != "" && p.About == "" {
		p.About = cleanText(person.Description)
	}
	if p.Location == "" {
		loc := strings.Join(filterEmpty([]string{
			person.Address.AddressLocality,
			person.Address.AddressRegion,
			person.Address.AddressCountry,
		}), ", ")
		p.Location = loc
	}
	if person.WorksFor.Name != "" && len(p.Experience) == 0 {
		p.Experience = append(p.Experience, Experience{
			Company: person.WorksFor.Name,
			Title:   person.JobTitle,
		})
	}
	for _, a := range person.AlumniOf {
		if a.Name != "" {
			p.Education = append(p.Education, Education{School: a.Name})
		}
	}
}

// extractHTML parses the HTML body with goquery, scoping each section properly.
func extractHTML(body string, p *Profile) error {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return err
	}

	extractTopCard(doc, p)
	extractAbout(doc, p)
	extractExperience(doc, p)
	extractEducation(doc, p)
	extractCertifications(doc, p)
	extractProjects(doc, p)
	extractLanguages(doc, p)
	extractSkills(doc, p)

	return nil
}

func extractTopCard(doc *goquery.Document, p *Profile) {
	if p.Name == "" {
		for _, sel := range []string{"h1.top-card-layout__title", "h1[class*='top-card']", "h1.text-heading-xlarge", "h1"} {
			if name := clean(doc.Find(sel).First().Text()); name != "" {
				p.Name = name
				break
			}
		}
	}

	if p.Headline == "" {
		for _, sel := range []string{"h2.top-card-layout__headline", "div.top-card-layout__headline", "div[class*='top-card__headline']"} {
			if h := clean(doc.Find(sel).First().Text()); h != "" && !isObfuscated(h) {
				p.Headline = h
				break
			}
		}
	}

	if p.Location == "" {
		doc.Find("span.top-card__subline-item, span[class*='subline-item']").Each(func(_ int, s *goquery.Selection) {
			text := clean(s.Text())
			if text != "" && !strings.Contains(strings.ToLower(text), "connection") && p.Location == "" && !isObfuscated(text) {
				p.Location = text
			}
		})
	}
}

func extractAbout(doc *goquery.Document, p *Profile) {
	if p.About != "" {
		return
	}
	// Try the summary/about section scoped selectors.
	candidates := []string{
		"section[data-section='summary'] .core-section-container__content p",
		"section[data-section='summary'] p",
		"div[data-section='summary'] p",
		"section.summary .summary__text",
		".summary__text",
	}
	for _, sel := range candidates {
		text := clean(doc.Find(sel).First().Text())
		if text != "" && !isObfuscated(text) {
			p.About = text
			return
		}
	}
}

// findSection returns the first section element matched by any of the CSS
// selectors, or — if none match — any <section> whose h2 heading contains
// the given keyword (case-insensitive).
func findSection(doc *goquery.Document, keyword string, cssSelectors []string) *goquery.Selection {
	for _, sel := range cssSelectors {
		s := doc.Find(sel)
		if s.Length() > 0 {
			return s.First()
		}
	}
	// Fallback: scan every <section> for a heading that contains the keyword.
	var found *goquery.Selection
	doc.Find("section").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		heading := strings.ToLower(clean(s.Find("h2, h3").First().Text()))
		if strings.Contains(heading, keyword) {
			found = s
			return false // stop
		}
		return true
	})
	return found
}

// parseOneExperienceItem extracts fields from a single experience <li> element.
func parseOneExperienceItem(s *goquery.Selection) (exp Experience, ok bool) {
	// Title: h3 or dedicated class
	title := clean(s.Find("h3, .experience-item__title, .profile-section-card__title").First().Text())

	// Company: h4 or dedicated class
	company := clean(s.Find("h4, .experience-item__subtitle, .profile-section-card__subtitle").First().Text())

	// Duration: LinkedIn public profiles put date ranges in several possible elements.
	// Try each in order of specificity.
	duration := ""
	for _, sel := range []string{
		".date-range",
		".experience-item__duration",
		"span.experience-item__date-range",
		"p.experience-item__meta-item",
		".profile-section-card__meta",
		"p[class*='meta']",
		"span[class*='date']",
		"time",
	} {
		if v := clean(s.Find(sel).First().Text()); v != "" && !isObfuscated(v) {
			duration = v
			break
		}
	}

	// Location: second meta-item paragraph, or dedicated selectors.
	location := ""
	for _, sel := range []string{
		".experience-item__location",
		".location",
		"span[class*='location']",
	} {
		if v := clean(s.Find(sel).First().Text()); v != "" && !isObfuscated(v) {
			location = v
			break
		}
	}
	// If still empty, try the second p.experience-item__meta-item (first = date, second = location).
	if location == "" {
		metaItems := s.Find("p.experience-item__meta-item")
		if metaItems.Length() >= 2 {
			if v := clean(metaItems.Eq(1).Text()); v != "" && !isObfuscated(v) {
				location = v
			}
		}
	}

	// Description
	desc := ""
	for _, sel := range []string{
		".experience-item__description",
		".profile-section-card__description",
		"p.description",
	} {
		if v := clean(s.Find(sel).First().Text()); v != "" && !isObfuscated(v) {
			desc = v
			break
		}
	}

	if isObfuscated(title) {
		title = ""
	}
	if isObfuscated(company) {
		company = ""
	}
	if title == "" && company == "" {
		return exp, false
	}
	return Experience{
		Title:    title,
		Company:  company,
		Duration: duration,
		Location: location,
		Desc:     desc,
	}, true
}

func extractExperience(doc *goquery.Document, p *Profile) {
	section := findSection(doc, "experience", []string{
		"section[data-section='experience']",
		"section.experience-section",
		"div[data-section='experience']",
	})

	var entries []Experience

	collectItems := func(root *goquery.Selection) {
		items := root.Find("li.experience-item")
		if items.Length() == 0 {
			items = root.Find("li.profile-section-card")
		}
		if items.Length() == 0 {
			items = root.Find("li")
		}
		items.Each(func(_ int, s *goquery.Selection) {
			if exp, ok := parseOneExperienceItem(s); ok {
				entries = append(entries, exp)
			}
		})
	}

	if section != nil {
		collectItems(section)
	}

	// Global fallback using the specific experience-item class.
	if len(entries) == 0 {
		doc.Find("li.experience-item").Each(func(_ int, s *goquery.Selection) {
			if exp, ok := parseOneExperienceItem(s); ok {
				entries = append(entries, exp)
			}
		})
	}

	// Only replace JSON-LD seeded data when HTML parsing found something.
	if len(entries) > 0 {
		p.Experience = entries
	}
}

func extractEducation(doc *goquery.Document, p *Profile) {
	section := findSection(doc, "education", []string{
		"section[data-section='education']",
		"section.education-section",
		"div[data-section='education']",
	})

	// Build a set of company names already captured in experience so we can
	// avoid duplicating them in the education list.
	expCompanies := make(map[string]bool)
	for _, exp := range p.Experience {
		if exp.Company != "" {
			expCompanies[strings.ToLower(exp.Company)] = true
		}
	}

	var entries []Education

	addEdu := func(s *goquery.Selection) {
		school := clean(s.Find("h3, .profile-section-card__title").First().Text())
		degree := clean(s.Find("h4, .profile-section-card__subtitle").First().Text())
		years := clean(s.Find(".date-range, .education__item--duration").First().Text())
		if isObfuscated(school) || school == "" {
			return
		}
		// Skip if this name already appeared as an employer.
		if expCompanies[strings.ToLower(school)] {
			return
		}
		if isObfuscated(degree) {
			degree = ""
		}
		entries = append(entries, Education{School: school, Degree: degree, Years: years})
	}

	if section != nil {
		// Only use the specific education class — never generic li which bleeds into other sections.
		items := section.Find("li.education__list-item")
		items.Each(func(_ int, s *goquery.Selection) { addEdu(s) })
	}

	// Global fallback using the specific education class only (not generic li).
	if len(entries) == 0 {
		doc.Find("li.education__list-item").Each(func(_ int, s *goquery.Selection) { addEdu(s) })
	}

	if len(entries) > 0 {
		p.Education = entries
	}
}


func extractCertifications(doc *goquery.Document, p *Profile) {
	section := findSection(doc, "certif", []string{
		"section[data-section='certifications']",
		"section.certifications-section",
		"div[data-section='certifications']",
	})
	if section == nil {
		return
	}
	section.Find("li").Each(func(_ int, s *goquery.Selection) {
		name := clean(s.Find("h3, .profile-section-card__title").First().Text())
		issuer := clean(s.Find("h4, .profile-section-card__subtitle").First().Text())
		date := clean(s.Find(".date-range, time").First().Text())
		if name != "" && !isObfuscated(name) {
			p.Certifications = append(p.Certifications, Certification{Name: name, Issuer: issuer, Date: date})
		}
	})
}

func extractProjects(doc *goquery.Document, p *Profile) {
	section := findSection(doc, "project", []string{
		"section[data-section='projects']",
		"section.projects-section",
		"div[data-section='projects']",
	})
	if section == nil {
		return
	}
	section.Find("li").Each(func(_ int, s *goquery.Selection) {
		name := clean(s.Find("h3, .profile-section-card__title").First().Text())
		desc := clean(s.Find("p, .profile-section-card__description").First().Text())
		if name != "" && !isObfuscated(name) {
			p.Projects = append(p.Projects, Project{Name: name, Desc: desc})
		}
	})
}

func extractLanguages(doc *goquery.Document, p *Profile) {
	section := findSection(doc, "language", []string{
		"section[data-section='languages']",
		"section.languages-section",
		"div[data-section='languages']",
	})
	if section == nil {
		return
	}
	section.Find("li").Each(func(_ int, s *goquery.Selection) {
		name := clean(s.Find("h3, .profile-section-card__title").First().Text())
		proficiency := clean(s.Find("h4, .profile-section-card__subtitle").First().Text())
		if name != "" && !isObfuscated(name) {
			p.Languages = append(p.Languages, Language{Name: name, Proficiency: proficiency})
		}
	})
}

func extractSkills(doc *goquery.Document, p *Profile) {
	seen := make(map[string]bool)
	doc.Find("li.skills-section__skill, span.skill-categories-and-top-skills__skill-item").Each(func(_ int, s *goquery.Selection) {
		skill := clean(s.Text())
		if skill != "" && !isObfuscated(skill) && !seen[skill] {
			seen[skill] = true
			p.Skills = append(p.Skills, skill)
		}
	})
}

// FormatAsCV converts the profile into a plain-text CV.
func (p *Profile) FormatAsCV() string {
	var sb strings.Builder

	sb.WriteString("=== LinkedIn Profile (used as CV) ===\n\n")

	if p.Name != "" {
		sb.WriteString("NAME: " + p.Name + "\n")
	}
	if p.Headline != "" {
		sb.WriteString("HEADLINE: " + p.Headline + "\n")
	}
	if p.Location != "" {
		sb.WriteString("LOCATION: " + p.Location + "\n")
	}
	if p.URL != "" {
		sb.WriteString("LINKEDIN: " + p.URL + "\n")
	}
	sb.WriteString("\n")

	if p.About != "" {
		sb.WriteString("ABOUT:\n" + p.About + "\n\n")
	}

	if len(p.Experience) > 0 {
		sb.WriteString("EXPERIENCE:\n")
		for _, exp := range p.Experience {
			line := exp.Title
			if exp.Company != "" {
				if line != "" {
					line += " @ " + exp.Company
				} else {
					line = exp.Company
				}
			}
			if exp.Duration != "" {
				line += " (" + exp.Duration + ")"
			}
			sb.WriteString("- " + line + "\n")
			if exp.Location != "" {
				sb.WriteString("  Location: " + exp.Location + "\n")
			}
			if exp.Desc != "" {
				sb.WriteString("  " + exp.Desc + "\n")
			}
		}
		sb.WriteString("\n")
	}

	if len(p.Education) > 0 {
		sb.WriteString("EDUCATION:\n")
		for _, edu := range p.Education {
			line := edu.School
			if edu.Degree != "" {
				line += " — " + edu.Degree
			}
			if edu.Field != "" {
				line += " in " + edu.Field
			}
			if edu.Years != "" {
				line += " (" + edu.Years + ")"
			}
			sb.WriteString("- " + line + "\n")
		}
		sb.WriteString("\n")
	}

	if len(p.Certifications) > 0 {
		sb.WriteString("CERTIFICATIONS:\n")
		for _, c := range p.Certifications {
			line := c.Name
			if c.Issuer != "" {
				line += " — " + c.Issuer
			}
			if c.Date != "" {
				line += " (" + c.Date + ")"
			}
			sb.WriteString("- " + line + "\n")
		}
		sb.WriteString("\n")
	}

	if len(p.Projects) > 0 {
		sb.WriteString("PROJECTS:\n")
		for _, proj := range p.Projects {
			sb.WriteString("- " + proj.Name + "\n")
			if proj.Desc != "" {
				sb.WriteString("  " + proj.Desc + "\n")
			}
		}
		sb.WriteString("\n")
	}

	if len(p.Languages) > 0 {
		sb.WriteString("LANGUAGES:\n")
		for _, lang := range p.Languages {
			line := lang.Name
			if lang.Proficiency != "" {
				line += " (" + lang.Proficiency + ")"
			}
			sb.WriteString("- " + line + "\n")
		}
		sb.WriteString("\n")
	}

	if len(p.Skills) > 0 {
		sb.WriteString("SKILLS:\n" + strings.Join(p.Skills, ", ") + "\n")
	}

	return sb.String()
}

// clean trims whitespace and removes "Show more" / "Show less" UI noise.
func clean(s string) string {
	s = showMoreRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// cleanText is like clean but also collapses internal newlines.
func cleanText(s string) string {
	return clean(s)
}

// isObfuscated returns true if more than 35% of non-space characters are '*',
// which indicates LinkedIn has hidden the content from unauthenticated users.
func isObfuscated(text string) bool {
	total, stars := 0, 0
	for _, c := range text {
		if c == ' ' || c == '\t' || c == '\n' {
			continue
		}
		total++
		if c == '*' {
			stars++
		}
	}
	if total == 0 {
		return false
	}
	return float64(stars)/float64(total) > 0.35
}

func filterEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}
