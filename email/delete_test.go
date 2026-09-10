package email

import (
	"strings"
	"testing"
)

// Endgültig heißt HardDelete — MoveToDeletedItems legte nur in den
// Papierkorb, und ein abgeschickter Entwurf hätte dort ein Doppel hinterlassen.
func TestDeleteItemArt(t *testing.T) {
	hart := deleteItemXML("abc", "HardDelete")
	if !strings.Contains(hart, `DeleteType="HardDelete"`) || !strings.Contains(hart, `Id="abc"`) {
		t.Fatalf("falsche Anfrage:\n%s", hart)
	}
	weich := deleteItemXML("abc", "MoveToDeletedItems")
	if !strings.Contains(weich, `DeleteType="MoveToDeletedItems"`) {
		t.Fatalf("falsche Anfrage:\n%s", weich)
	}
}

// Die Kennung wird maskiert — sie kommt von außen.
func TestDeleteItemMaskiert(t *testing.T) {
	x := deleteItemXML(`a"<b`, "HardDelete")
	if strings.Contains(x, `a"<b`) {
		t.Fatalf("nicht maskiert:\n%s", x)
	}
}
