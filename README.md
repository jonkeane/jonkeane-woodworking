# Woodworking site for jonkeane

## Installation

```
hugo mod get
hugo mod npm pack
npm install
```

##
TailwindCSS needs to poke at each html file to know what to include in the CSS. In order to have both our overrides and the theme's html files, we need to `hugo mod vendor` during build and then point tailwind.config.js at the theme's layouts as well as our own.

## Flickr API

In order to use the flickr API to get image details, one must set the params.nanog.flickr_api with a valid flickr API key one can also set HUGOxPARAMSxNANOGxflickr_apikey (for e.g. netlify's deploy)