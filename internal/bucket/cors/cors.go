// Copyright (c) 2015-2021 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

// Package cors implements the S3 per-bucket CORS configuration type,
// its validation, and origin/method/header matching helpers.
package cors

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxCORSRules is the maximum number of rules allowed per bucket (AWS S3 limit).
const maxCORSRules = 100

// maxCORSRuleIDLen is the maximum length of a CORSRule <ID> (AWS S3 limit).
const maxCORSRuleIDLen = 255

// maxCORSMaxAgeSeconds is the largest value representable by the int32
// MaxAgeSeconds shape used by the S3 API model.
const maxCORSMaxAgeSeconds = 1<<31 - 1

// supportedMethods are the HTTP methods permitted in an AllowedMethod element.
var supportedMethods = map[string]bool{
	"GET":    true,
	"PUT":    true,
	"HEAD":   true,
	"POST":   true,
	"DELETE": true,
}

// Config is the S3 <CORSConfiguration> document.
type Config struct {
	XMLName   xml.Name `xml:"CORSConfiguration"`
	CORSRules []Rule   `xml:"CORSRule"`
}

// Rule is a single <CORSRule>.
type Rule struct {
	ID             string   `xml:"ID,omitempty"`
	AllowedHeaders []string `xml:"AllowedHeader"`
	AllowedMethods []string `xml:"AllowedMethod"`
	AllowedOrigins []string `xml:"AllowedOrigin"`
	ExposeHeaders  []string `xml:"ExposeHeader"`
	MaxAgeSeconds  int      `xml:"MaxAgeSeconds"`

	maxAgeSecondsSet bool
}

type corsXMLUnknown struct {
	XMLName xml.Name
}

type corsXMLValue struct {
	Text    string           `xml:",chardata"`
	Unknown []corsXMLUnknown `xml:",any"`
}

type configXML struct {
	XMLName   xml.Name         `xml:"CORSConfiguration"`
	CORSRules []ruleXML        `xml:"CORSRule"`
	Text      string           `xml:",chardata"`
	Unknown   []corsXMLUnknown `xml:",any"`
}

type ruleXML struct {
	ID             []corsXMLValue   `xml:"ID"`
	AllowedHeaders []corsXMLValue   `xml:"AllowedHeader"`
	AllowedMethods []corsXMLValue   `xml:"AllowedMethod"`
	AllowedOrigins []corsXMLValue   `xml:"AllowedOrigin"`
	ExposeHeaders  []corsXMLValue   `xml:"ExposeHeader"`
	MaxAgeSeconds  []corsXMLValue   `xml:"MaxAgeSeconds"`
	Text           string           `xml:",chardata"`
	Unknown        []corsXMLUnknown `xml:",any"`
}

// ParseBucketCorsConfig parses a CORS configuration from the given reader.
func ParseBucketCorsConfig(r io.Reader) (*Config, error) {
	var parsed configXML
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(&parsed); err != nil {
		return nil, err
	}
	if strings.TrimSpace(parsed.Text) != "" {
		return nil, xml.UnmarshalError("unexpected character data in CORSConfiguration")
	}
	if len(parsed.Unknown) > 0 {
		return nil, xml.UnmarshalError(fmt.Sprintf("unexpected element <%s> in CORSConfiguration", parsed.Unknown[0].XMLName.Local))
	}

	c := Config{
		XMLName:   parsed.XMLName,
		CORSRules: make([]Rule, len(parsed.CORSRules)),
	}
	for i := range parsed.CORSRules {
		rule, err := parseCORSRuleXML(parsed.CORSRules[i])
		if err != nil {
			return nil, err
		}
		c.CORSRules[i] = rule
	}

	// Decode consumes one document element. Only XML whitespace, comments, and
	// processing instructions are permitted after it.
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch token := token.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(token)) == "" {
				continue
			}
		case xml.Comment, xml.ProcInst:
			continue
		}
		return nil, errors.New("unexpected XML content after CORSConfiguration")
	}
	return &c, nil
}

func parseCORSRuleXML(parsed ruleXML) (Rule, error) {
	if strings.TrimSpace(parsed.Text) != "" {
		return Rule{}, xml.UnmarshalError("unexpected character data in CORSRule")
	}
	if len(parsed.Unknown) > 0 {
		return Rule{}, xml.UnmarshalError(fmt.Sprintf("unexpected element <%s> in CORSRule", parsed.Unknown[0].XMLName.Local))
	}
	if len(parsed.ID) > 1 {
		return Rule{}, xml.UnmarshalError("duplicate ID element in CORSRule")
	}
	if len(parsed.MaxAgeSeconds) > 1 {
		return Rule{}, xml.UnmarshalError("duplicate MaxAgeSeconds element in CORSRule")
	}

	rule := Rule{}
	var err error
	if len(parsed.ID) == 1 {
		if rule.ID, err = corsXMLText("ID", parsed.ID[0]); err != nil {
			return Rule{}, err
		}
	}
	if rule.AllowedHeaders, err = corsXMLTexts("AllowedHeader", parsed.AllowedHeaders); err != nil {
		return Rule{}, err
	}
	if rule.AllowedMethods, err = corsXMLTexts("AllowedMethod", parsed.AllowedMethods); err != nil {
		return Rule{}, err
	}
	if rule.AllowedOrigins, err = corsXMLTexts("AllowedOrigin", parsed.AllowedOrigins); err != nil {
		return Rule{}, err
	}
	if rule.ExposeHeaders, err = corsXMLTexts("ExposeHeader", parsed.ExposeHeaders); err != nil {
		return Rule{}, err
	}
	if len(parsed.MaxAgeSeconds) == 1 {
		value, valueErr := corsXMLText("MaxAgeSeconds", parsed.MaxAgeSeconds[0])
		if valueErr != nil {
			return Rule{}, valueErr
		}
		age, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
		if parseErr != nil {
			return Rule{}, xml.UnmarshalError("invalid MaxAgeSeconds value")
		}
		rule.MaxAgeSeconds = int(age)
		rule.maxAgeSecondsSet = true
	}
	return rule, nil
}

func corsXMLTexts(name string, values []corsXMLValue) ([]string, error) {
	result := make([]string, len(values))
	for i := range values {
		value, err := corsXMLText(name, values[i])
		if err != nil {
			return nil, err
		}
		result[i] = value
	}
	return result, nil
}

func corsXMLText(name string, value corsXMLValue) (string, error) {
	if len(value.Unknown) > 0 {
		return "", xml.UnmarshalError(fmt.Sprintf("element <%s> must not contain child element <%s>", name, value.Unknown[0].XMLName.Local))
	}
	return value.Text, nil
}

// Validate checks the config against the S3 constraints.
func (c *Config) Validate() error {
	if len(c.CORSRules) == 0 {
		return errors.New("CORSConfiguration must contain at least one rule")
	}
	if len(c.CORSRules) > maxCORSRules {
		return errors.New("CORSConfiguration exceeds the maximum number of rules")
	}
	for _, r := range c.CORSRules {
		if !utf8.ValidString(r.ID) {
			return errors.New("CORSRule ID must contain valid UTF-8")
		}
		if utf8.RuneCountInString(r.ID) > maxCORSRuleIDLen {
			return errors.New("CORSRule ID exceeds the maximum length of 255 characters")
		}
		if len(r.AllowedOrigins) == 0 {
			return errors.New("CORSRule must contain at least one AllowedOrigin")
		}
		if len(r.AllowedMethods) == 0 {
			return errors.New("CORSRule must contain at least one AllowedMethod")
		}
		for _, o := range r.AllowedOrigins {
			if o == "" {
				return errors.New("AllowedOrigin must not be empty")
			}
			if strings.Contains(o, "?") {
				return errors.New("AllowedOrigin may not contain wildcard '?': " + o)
			}
			if strings.Count(o, "*") > 1 {
				return errors.New("AllowedOrigin may contain at most one wildcard '*': " + o)
			}
		}
		for _, m := range r.AllowedMethods {
			if !supportedMethods[m] {
				return errors.New("unsupported method in CORSRule: " + m)
			}
		}
		for _, h := range r.AllowedHeaders {
			if h == "" {
				return errors.New("AllowedHeader must not be empty")
			}
			if strings.Contains(h, "?") {
				return errors.New("AllowedHeader may not contain wildcard '?': " + h)
			}
			if strings.Count(h, "*") > 1 {
				return errors.New("AllowedHeader may contain at most one wildcard '*': " + h)
			}
		}
		for _, h := range r.ExposeHeaders {
			if h == "" {
				return errors.New("ExposeHeader must not be empty")
			}
		}
		if r.MaxAgeSeconds < 0 {
			return errors.New("MaxAgeSeconds must not be negative")
		}
		if int64(r.MaxAgeSeconds) > maxCORSMaxAgeSeconds {
			return errors.New("MaxAgeSeconds exceeds the maximum S3 integer value")
		}
	}
	return nil
}

func matchSingleWildcard(pattern, value string) bool {
	prefix, suffix, found := strings.Cut(pattern, "*")
	if !found {
		return pattern == value
	}
	return len(value) >= len(prefix)+len(suffix) &&
		strings.HasPrefix(value, prefix) && strings.HasSuffix(value, suffix)
}

func (r Rule) matchAllowedOrigin(origin string) (string, bool) {
	for _, allowedOrigin := range r.AllowedOrigins {
		if matchSingleWildcard(allowedOrigin, origin) {
			return allowedOrigin, true
		}
	}
	return "", false
}

// HasAllowedMethod reports whether the rule allows the given HTTP method.
func (r Rule) HasAllowedMethod(method string) bool {
	for _, m := range r.AllowedMethods {
		if m == method {
			return true
		}
	}
	return false
}

// FilterAllowedHeaders returns the subset of reqHeaders permitted by the rule
// and whether every requested header was allowed.
func (r Rule) FilterAllowedHeaders(reqHeaders []string) ([]string, bool) {
	var allowed []string
	for _, h := range reqHeaders {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if !r.headerAllowed(h) {
			return nil, false
		}
		allowed = append(allowed, h)
	}
	return allowed, true
}

func (r Rule) headerAllowed(header string) bool {
	for _, h := range r.AllowedHeaders {
		if matchSingleWildcard(strings.ToLower(h), strings.ToLower(header)) {
			return true
		}
	}
	return false
}

// MatchRule returns the first rule whose origin and method both match, along
// with the configured origin pattern that matched.
func (c *Config) MatchRule(origin, method string) (rule *Rule, allowedOrigin string, ok bool) {
	for i := range c.CORSRules {
		r := &c.CORSRules[i]
		matchedOrigin, originOK := r.matchAllowedOrigin(origin)
		if originOK && r.HasAllowedMethod(method) {
			return r, matchedOrigin, true
		}
	}
	return nil, "", false
}

// MatchPreflight returns the first rule whose origin and method match and
// whose AllowedHeaders permit every header in reqHeaders. Unlike MatchRule,
// this keeps evaluating subsequent rules until one fully satisfies the
// preflight request, since an earlier origin/method match with a more
// restrictive header list must not shadow a later, more permissive rule.
func (c *Config) MatchPreflight(origin, method string, reqHeaders []string) (rule *Rule, allowedOrigin string, allowedHeaders []string, maxAgeSeconds *int, ok bool) {
	for i := range c.CORSRules {
		r := &c.CORSRules[i]
		matchedOrigin, originOK := r.matchAllowedOrigin(origin)
		if !originOK || !r.HasAllowedMethod(method) {
			continue
		}
		allowed, headersOK := r.FilterAllowedHeaders(reqHeaders)
		if !headersOK {
			continue
		}
		if r.maxAgeSecondsSet || r.MaxAgeSeconds != 0 {
			maxAgeSeconds = &r.MaxAgeSeconds
		}
		return r, matchedOrigin, allowed, maxAgeSeconds, true
	}
	return nil, "", nil, nil, false
}
