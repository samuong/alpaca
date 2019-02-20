package main

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"net/url"
	"strings"
	"testing"
)

func TestDirect(t *testing.T) {
	src := `function FindProxyForURL(url, host) { return "DIRECT"; }`
	pf, err := NewProxyFinder(strings.NewReader(src))
	require.Nil(t, err)
	u, err := url.Parse("https://www.anz.com.au/personal/")
	require.Nil(t, err)
	proxy, err := pf.FindProxyForURL(u)
	require.Nil(t, err)
	assert.Equal(t, "DIRECT", proxy)
}

type IsPlainHostNameSuite struct {
	suite.Suite
	pf *ProxyFinder
}

func (suite *IsPlainHostNameSuite) SetupTest() {
	src := `
		function FindProxyForURL(url, host) {
			if (isPlainHostName(host))
				return "isPlainHostName is true";
			else
				return "isPlainHostName is false";
		}
	`
	var err error
	suite.pf, err = NewProxyFinder(strings.NewReader(src))
	suite.Require().Nil(err)
}

func (suite *IsPlainHostNameSuite) TestIsPlainHostName() {
	u, err := url.Parse("https://www")
	suite.Require().Nil(err)
	proxy, err := suite.pf.FindProxyForURL(u)
	suite.Require().Nil(err)
	suite.Assert().Equal("isPlainHostName is true", proxy)
}

func (suite *IsPlainHostNameSuite) TestIsNotPlainHostName() {
	u, err := url.Parse("https://www.mozilla.org")
	suite.Require().Nil(err)
	proxy, err := suite.pf.FindProxyForURL(u)
	suite.Require().Nil(err)
	suite.Assert().Equal("isPlainHostName is false", proxy)
}

func TestIsPlainHostNameSuite(t *testing.T) {
	suite.Run(t, new(IsPlainHostNameSuite))
}
