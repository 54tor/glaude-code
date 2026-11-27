package main

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOrphansRoutesTrashRestorePurge(t *testing.T) {
	handler, _, _ := web(t)
	deposerFichier(t, "a.png", 2*time.Hour)
	liste := v1ok(t, handler, "GET", "/v1/uploads/orphans", "", "a.png")
	if fichiers, _ := liste["files"].([]any); len(fichiers) != 1 {
		t.Fatalf("liste : %v", liste)
	}
	v1ok(t, handler, "POST", "/v1/uploads/orphans?item=a.png", "", "")
	if !existe(filepath.Join(orphanTrashDir(), "a.png")) {
		t.Fatal("fichier absent de la corbeille")
	}
	v1ok(t, handler, "PATCH", "/v1/uploads/orphans?item=a.png", "", "")
	if !existe(filepath.Join(imagesDir(), "a.png")) {
		t.Fatal("fichier non restauré")
	}
	v1ok(t, handler, "POST", "/v1/uploads/orphans?item=a.png", "", "")
	v1ok(t, handler, "DELETE", "/v1/uploads/orphans?item=a.png", "", "")
	if existe(filepath.Join(orphanTrashDir(), "a.png")) {
		t.Fatal("fichier non purgé")
	}
}

func TestOrphansRoutesDeleteWithoutItemOrAllIsRejected(t *testing.T) {
	handler, _, _ := web(t)
	deposerFichier(t, "a.png", 2*time.Hour)
	trashOrphan("a.png")
	status(t, handler, "DELETE", "/v1/uploads/orphans", "", 400)
	if !existe(filepath.Join(orphanTrashDir(), "a.png")) {
		t.Fatal("corbeille vidée par une requête sans cible")
	}
}

func TestOrphansRoutesDeleteAllEmptiesTrashOnly(t *testing.T) {
	handler, _, _ := web(t)
	deposerFichier(t, "a.png", 2*time.Hour)
	deposerFichier(t, "b.png", 2*time.Hour)
	trashOrphan("a.png")
	v1ok(t, handler, "DELETE", "/v1/uploads/orphans?all=1", "", "")
	if existe(filepath.Join(orphanTrashDir(), "a.png")) || !existe(filepath.Join(imagesDir(), "b.png")) {
		t.Fatal("?all=1 doit vider la corbeille sans toucher aux fichiers actifs")
	}
}

func TestOrphansRoutesRejectHostileName(t *testing.T) {
	handler, _, _ := web(t)
	dehors := filepath.Join(filepath.Dir(imagesDir()), "secret.txt")
	os.WriteFile(dehors, []byte("x"), 0o600)
	status(t, handler, "POST", "/v1/uploads/orphans?item="+url.QueryEscape("../secret.txt"), "", 400)
	status(t, handler, "DELETE", "/v1/uploads/orphans?item="+url.QueryEscape("../secret.txt"), "", 400)
	if !existe(dehors) {
		t.Fatal("fichier hors dossier touché")
	}
}
