package email

import (
	"reflect"
	"strings"
	"testing"
)

// Bcc wird nur geschrieben, wenn etwas da ist — sonst löschte ein Speichern
// aus dem Schreibfenster, was Outlook dort eingetragen hat.
func TestDraftUpdateXMLBccNurWennDa(t *testing.T) {
	ohne := draftUpdateXML("id1", []string{"a@x.de"}, nil, nil, "B", "T", "Text")
	if strings.Contains(ohne, "BccRecipients") {
		t.Fatalf("Bcc angefasst, obwohl keiner angegeben:\n%s", ohne)
	}
	mit := draftUpdateXML("id1", []string{"a@x.de"}, nil, []string{"b@x.de"}, "B", "T", "Text")
	if !strings.Contains(mit, `FieldURI="message:BccRecipients"`) || !strings.Contains(mit, "b@x.de") {
		t.Fatalf("Bcc fehlt:\n%s", mit)
	}
}

// An und Cc stehen im Schreibfenster; leer heißt dort: keiner.
func TestDraftUpdateXMLLeertAnUndCc(t *testing.T) {
	x := draftUpdateXML("id1", nil, nil, nil, "B", "T", "HTML")
	if !strings.Contains(x, "<t:DeleteItemField>\n      <t:FieldURI FieldURI=\"message:ToRecipients\"/>") ||
		!strings.Contains(x, "<t:DeleteItemField>\n      <t:FieldURI FieldURI=\"message:CcRecipients\"/>") {
		t.Fatalf("An/Cc nicht geleert:\n%s", x)
	}
	if !strings.Contains(x, `MessageDisposition="SaveOnly"`) || !strings.Contains(x, `BodyType="HTML"`) {
		t.Fatalf("falsche Anfrage:\n%s", x)
	}
}

// Betreff und Text werden maskiert — sie kommen aus dem Schreibfenster.
func TestDraftUpdateXMLMaskiert(t *testing.T) {
	x := draftUpdateXML(`i"d`, nil, nil, nil, "<b>&", "<script>", "Text")
	for _, roh := range []string{`i"d`, "<b>&", "<script>"} {
		if strings.Contains(x, roh) {
			t.Fatalf("%q steht unmaskiert in der Anfrage", roh)
		}
	}
}

func TestAdressen(t *testing.T) {
	got := adressen([]ewsMailbox{{Name: "A", EmailAddress: "a@x.de"}, {Name: "Nur Name"}, {}})
	if !reflect.DeepEqual(got, []string{"a@x.de", "Nur Name"}) {
		t.Fatalf("adressen = %v", got)
	}
	if adressen(nil) != nil {
		t.Fatal("leere Liste ergibt nicht nil")
	}
}
