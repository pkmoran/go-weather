package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	mw := multiWeatherProvider{
		openWeatherMap{apiKey: os.Getenv("OPEN_WEATHER_MAP_KEY")},
		weatherUnderground{apiKey: os.Getenv("WEATHER_UNDERGROUND_KEY")},
		darkSky{
			apiKey:    os.Getenv("DARK_SKY_KEY"),
			googleKey: os.Getenv("GOOGLE_GEOCODE_KEY"),
		},
	}

	http.HandleFunc("/hello", hello)

	http.HandleFunc("/weather/", func(w http.ResponseWriter, r *http.Request) {
		begin := time.Now()
		city := strings.SplitN(r.URL.Path, "/", 3)[2]

		temp, err := mw.temperature(city)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"city": city,
			"temp": int(temp),
			"took": time.Since(begin).String(),
		})
	})

	http.ListenAndServe(":8080", nil)
}

func hello(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("hello!"))
}

type weatherProvider interface {
	temperature(city string) (float64, error) // Kelvin
}

type multiWeatherProvider []weatherProvider

type openWeatherMap struct {
	apiKey string
}

type weatherUnderground struct {
	apiKey string
}

type darkSky struct {
	apiKey    string
	googleKey string
}

func (w openWeatherMap) temperature(city string) (float64, error) {
	resp, err := http.Get("http://api.openweathermap.org/data/2.5/weather?APPID=" + w.apiKey + "&q=" + city)
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	var d struct {
		Main struct {
			Kelvin float64 `json:"temp"`
		} `json:"main"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, err
	}

	log.Printf("openWeatherMap: %s: %.2f", city, d.Main.Kelvin)
	return d.Main.Kelvin, nil
}

func (w weatherUnderground) temperature(city string) (float64, error) {
	resp, err := http.Get("http://api.wunderground.com/api/" + w.apiKey + "/conditions/q/" + city + ".json")
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	var d struct {
		Observation struct {
			Celsius float64 `json:"temp_c"`
		} `json:"current_observation"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, err
	}

	kelvin := d.Observation.Celsius + 273.15
	log.Printf("weatherUnderground: %s: %.2f", city, kelvin)
	return kelvin, nil
}

func (w darkSky) temperature(city string) (float64, error) {
	lattitude, longitude, err := w.getCoords(city, w.googleKey)
	if err != nil {
		return 0, err
	}

	lat := fmt.Sprint(lattitude)
	lon := fmt.Sprint(longitude)

	resp, err := http.Get("https://api.darksky.net/forecast/" + w.apiKey + "/" + lat + "," + lon + "?exclude=minutely,hourly,daily,alerts,flags&units=si")
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	var d struct {
		Currently struct {
			Temperature float64
		}
	}

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, err
	}

	kelvin := d.Currently.Temperature + 273.15
	log.Printf("darkSky: %s: %.2f", city, kelvin)
	return kelvin, nil
}

func (w darkSky) getCoords(city string, key string) (float64, float64, error) {
	res, err := http.Get("https://maps.googleapis.com/maps/api/geocode/json?address=" + city + "&key=" + key)
	if err != nil {
		return 0, 0, err
	}

	defer res.Body.Close()

	var g struct {
		Results []struct {
			Geometry struct {
				Location struct {
					Lat float64
					Lng float64
				}
			}
		}
	}

	if err := json.NewDecoder(res.Body).Decode(&g); err != nil {
		return 0, 0, err
	}

	lat := g.Results[0].Geometry.Location.Lat
	lon := g.Results[0].Geometry.Location.Lng

	return lat, lon, err
}

func (w multiWeatherProvider) temperature(city string) (float64, error) {

	// Make a channel for temperatures, and a channel for errors.
	// Each provider will push a value into only one.
	temps := make(chan float64, len(w))
	errs := make(chan error, len(w))

	// For each provider, spawn a goroutine with an anonymous function.
	// That function will invoke the temperature method, and forward the response.
	for _, provider := range w {
		go func(p weatherProvider) {
			k, err := p.temperature(city)
			if err != nil {
				errs <- err
				return
			}
			temps <- k
		}(provider)
	}

	sum := 0.0

	// Collect a temperature or an error from each provider.
	for i := 0; i < len(w); i++ {
		select {
		case temp := <-temps:
			sum += temp
		case err := <-errs:
			return 0, err
		}
	}

	// Average the temps
	avg := sum / float64(len(w))

	// Convert to Celsius
	c := avg - 273.15
	// Convert to Fahrenheit
	f := c*1.8 + 32

	// Return the average.
	return f, nil
}

func temperature(city string, providers ...weatherProvider) (float64, error) {
	sum := 0.0

	for _, provider := range providers {
		k, err := provider.temperature(city)
		if err != nil {
			return 0, err
		}

		sum += k
	}

	return sum / float64(len(providers)), nil
}
