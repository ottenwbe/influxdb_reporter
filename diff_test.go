/*
Copyright (c) 2017 Beate Ottenwälder

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package main

import (
	"fmt"
	influxClient "github.com/influxdata/influxdb/client/v2"
	"github.com/sirupsen/logrus"
	"math/rand"
	"testing"
	"time"
)

func TestSimple(t *testing.T) {
	serie1, err := influxClient.NewPoint(
		"test_init",
		map[string]string{"tag0": "val0"},
		map[string]interface{}{"col0": 42},
		time.Now(),
	)

	if err != nil {
		logrus.Fatal("Cannot create point.")
	}

	if diffFromLast(serie1) != nil {
		t.Error("First time, initialization can't return a valid serie")
	}

	if serie1.Name() != "test_init" {
		t.Error("diffFromLast shouldn't modify serie name")
	}

	if len(serie1.Tags()) != 1 {
		t.Error("diffFromLast shouldn't modify tag number")
	}

	if _, ok := serie1.Tags()["tag0"]; !ok {
		t.Error("diffFromLast shouldn't modify tag name")
	}

	if v := serie1.Tags()["tag0"]; v != "val0" {
		t.Error("diffFromLast shouldn't modify tag value")
	}

	serie2, _ := influxClient.NewPoint(
		"test_init_other",
		map[string]string{"tag0": "val0", "tag1": "val1"},
		map[string]interface{}{"col1": 22, "col0": 23},
		time.Now(),
	)

	if diffFromLast(serie2) != nil {
		t.Error("Another serie (different serie name and tags) have to be initialized too")
	}

	serie1, _ = influxClient.NewPoint(
		"test_init",
		map[string]string{"tag0": "val0"},
		map[string]interface{}{"col0": 23},
		time.Now(),
	)

	serie2, _ = influxClient.NewPoint(
		"test_init_other",
		map[string]string{"tag0": "val0", "tag1": "val1"},
		map[string]interface{}{"col0": 43, "col1": 42},
		time.Now(),
	)

	if diffFromLast(serie1) == nil || diffFromLast(serie2) == nil {
		t.Error("Initialized diff serie shouldn't return nil")
	}

	fields1, _ := serie1.Fields()
	if fields1["col0"] != int64(23-42) {
		t.Error("Bad diff:", fields1["col0"], "!= 23 - 42")
	}

	fields2, _ := serie2.Fields()
	if fields2["col0"] != int64(43-23) {
		t.Error("Bad diff:", fields2["col0"], "!= 43 - 23")
	}
	if fields2["col1"] != int64(42-22) {
		t.Error("Bad diff:", fields2["col1"], "!= 42 - 22")
	}
}

func TestRandom(t *testing.T) {
	serie, err := influxClient.NewPoint(
		fmt.Sprint("test_rnd"),
		map[string]string{"toto": "titi"},
		map[string]interface{}{"random": 43, "data": 42},
		time.Now(),
	)

	if err != nil {
		logrus.WithError(err).Fatal("Cannot create point")
	}

	size := rand.Intn(30) + 12

	var oldPts, newPts map[string]interface{}
	newPts = make(map[string]interface{}, size)

	serie = fillPoints(serie, &newPts, size)

	diffFromLast(serie)

	for h := 0; h < rand.Intn(50)+10; h++ {
		oldPts = newPts
		newPts = make(map[string]interface{})

		serie = fillPoints(serie, &newPts, size)
		diffFromLast(serie)

		// Compare
		for i := 0; i < size; i++ {
			k := fmt.Sprint("col", i)
			fields, err := serie.Fields()
			if err != nil {
				logrus.WithError(err).Fatal("Cannot get fields")
			}

			if fields[k] != int64((newPts)[k].(int)-(oldPts)[k].(int)) {
				t.Errorf("Iteration %d; point %s: expected %d, got %d", h, k,
					(newPts)[k].(int)-(oldPts)[k].(int), fields[k])
			}
		}
	}
}

func fillPoints(serie *influxClient.Point, pts *map[string]interface{}, size int) *influxClient.Point {

	*pts = make(map[string]interface{}, size)
	for i := 0; i < size; i++ {
		tmp := rand.Intn(9876543210)
		k := fmt.Sprint("col", i)
		(*pts)[k] = tmp
	}

	res, _ := influxClient.NewPoint(
		serie.Name(),
		serie.Tags(),
		*pts,
		time.Now(),
	)
	return res

}
