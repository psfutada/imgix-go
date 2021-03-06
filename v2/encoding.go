package imgix

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

// checkProxyStatus checks if the path has one of the four possible
// acceptable proxy prefixes. First we check if the path has the
// correct ascii prefix. If it does then we know that it is a proxy,
// but it's not percent encoded. Second, we check if the path is
// prefixed by a percent-encoded prefix. If it is, we know that it's
// a proxy and that it's percent-encoded. Finally, if the path isn't
// prefixed by any of these four prefixes, it is not a valid proxy.
// This might be "just enough validation," but if we run into issues
// we can make this check smarter/more-robust.
func checkProxyStatus(p string) (isProxy bool, isEncoded bool) {
	path := p
	if strings.HasPrefix(p, "/") {
		path = p[1:]
	}

	const asciiHTTP = "http://"
	const asciiHTTPS = "https://"
	if strings.HasPrefix(path, asciiHTTP) || strings.HasPrefix(path, asciiHTTPS) {
		return true, false
	}

	const encodedHTTP = "http%3A%2F%2F"
	const encodedHTTPS = "https%3A%2F%2F"
	if strings.HasPrefix(path, encodedHTTP) || strings.HasPrefix(path, encodedHTTPS) {
		return true, true
	}

	const encodedHTTPLower = "http%3a%2f%2f"
	const encodedHTTPSLower = "https%3a%ff%2f"
	if strings.HasPrefix(path, encodedHTTPLower) || strings.HasPrefix(path, encodedHTTPSLower) {
		return true, true
	}

	return false, false
}

// encodeProxy will encode the given path string if it hasn't been
// encoded. If the path string isEncoded, then the path string is
// returned unchanged. Otherwise, the path is passed to PathEscape.
// The proxy-path is nearly escaped for our use-case after the call
// to PathEscape.
//
// Due to the way PathEscape works, we have to go back and percent
// encode colon characters (i.e. ':' to "%3A").
//
// See:
// https://golang.org/src/net/url/url.go?s=7851:7884#L137
func encodeProxy(proxyPath string, isEncoded bool) (escapedProxyPath string) {
	if isEncoded {
		return proxyPath
	}

	var nearlyEscaped string
	// The proxyPath should be prefixed by this point, but if it isn't check
	// and then do the right thing.
	if strings.HasPrefix(proxyPath, "/") {
		nearlyEscaped = "/" + url.PathEscape(proxyPath[1:])
	} else {
		nearlyEscaped = "/" + url.PathEscape(proxyPath)
	}

	escapedProxyPath = strings.ReplaceAll(nearlyEscaped, ":", "%3A")
	return escapedProxyPath
}

// encodePath uses splitAndEscape to encode the given path string into
// a form that can be safely placed inside a URL path segment. The path
// passed to this func should be prefixed with a '/', but if it isn't
// this function produces the same output.
func encodePath(path string) string {
	if strings.HasPrefix(path, "/") {
		escapedPath := splitAndEscape(path[1:])
		return "/" + escapedPath
	}
	return "/" + splitAndEscape(path)
}

// splitAndEscape splits the path on forward slash characters,
// PathEscape's each component, replaces any '+' with "%2B", then
// appends this escaped component to the results array. The result
// is then joined together using '/' as the delimeter.
func splitAndEscape(path string) string {
	if path == "" {
		return path
	}

	var result []string
	splitPath := strings.Split(path, "/")

	for _, component := range splitPath {
		c := url.PathEscape(component)
		pathEscaped := strings.ReplaceAll(c, "+", "%2B")
		result = append(result, pathEscaped)
	}

	return strings.Join(result, "/")
}

// encodeQueryString encodes a set of params into a form that can be
// safely used within the query string of a URL.
func encodeQuery(params url.Values) (encodedQueryParts []string) {

	keys := make([]string, 0, len(params))

	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		encodedKey, encodedValue := encodeQueryParam(k, params[k])
		encodedPairStr := strings.Join([]string{encodedKey, encodedValue}, "=")
		encodedQueryParts = append(encodedQueryParts, encodedPairStr)
	}
	return encodedQueryParts
}

// encodedQueryParam encodes a key and values into forms that can be
// safely placed within a URL query string. If the key has been
// suffixed with the base64 suffix, "64" (e.g. "text64"), then its
// corresponding value will be base64 encoded in a way that's safe
// for URLs.
func encodeQueryParam(key string, values []string) (eK string, eV string) {
	eK = encodeQueryParamValue(key)

	valuesLength := len(values)

	// If there are multiple values, then join them together
	// and then treat them as a single value.
	var value string
	if valuesLength > 1 {
		value = strings.Join(values, ",")
	} else if valuesLength == 1 {
		value = values[0]
	}

	if isBase64(key) {
		eV = base64EncodeQueryParamValue(value)
		return eK, eV
	}

	eV = encodeQueryParamValue(value)
	return eK, eV
}

// encodeQueryParamValue uses url.QueryEscape to escape the queryValue
// into a form that is safe to use in URLs. Note that net/url uses
// plus (+) as SPACE and does not percent-encode '+' to "%20".
func encodeQueryParamValue(queryValue string) string {
	return url.QueryEscape(queryValue)
}

// isBase64 checks if the paramKey is suffixed by "64," indicating
// that the value is intended to be base64-URL-encoded.
func isBase64(paramKey string) bool {
	return strings.HasSuffix(paramKey, "64")
}

// base64EncodeQueryParamValue base64 encodes the queryValue string. It
// does so in accordance with RFC 4648, which obsoletes RFC 3548. The
// important points are that the diff isn't significant for anything
// we care about.
//
// See:
// https://tools.ietf.org/rfcdiff?url2=rfc4648
func base64EncodeQueryParamValue(queryValue string) string {
	maybePaddedValue := base64.URLEncoding.EncodeToString([]byte(queryValue))
	return unPadBase64Value(maybePaddedValue)
}

// unPadBase64Value removes the extra '=' (equal signs) from strings.
// In base64, '=' are added to the end of the encoding as padding.
// This padding is significant if concatenating multiple base64-encoded
// strings. In our case, '&' acts as the primary delimeter and base64-encoded
// strings (query string values, usually) are dealt with individually
// (meaning that the length of the base64 encoded string is always
// known; this is important when decoding base64).
func unPadBase64Value(s string) string {
	if strings.HasSuffix(s, "=") {
		return strings.ReplaceAll(s, "=", "")
	}
	return s
}

// createMd5Signature creates the signature by joining the token, path, and params
// strings into a signatureBase. Next, create a hashedSig and write the
// signatureBase into it. Finally, return the encoded, signed string.
func createMd5Signature(token string, path string, query string) string {
	var delim string

	if query == "" {
		delim = ""
	} else {
		delim = "?"
	}

	// The expected signature base has the form:
	// {TOKEN}{PATH}{DELIM}{QUERY}
	signatureBase := strings.Join([]string{token, path, delim, query}, "")
	hashedSig := md5.New()
	hashedSig.Write([]byte(signatureBase))
	return hex.EncodeToString(hashedSig.Sum(nil))
}
