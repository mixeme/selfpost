package view

// pageFixtures is the data each redesigned page is rendered with by the guard
// tests (guard_panel_test.go): one entry per page that has left
// legacy_pages.txt, keyed by the page's engine name. The guards render the page
// through Engine.Render with this value and hold the result to the design
// contract — its class vocabulary, its page structure and the component
// skeleton recorded for the mockup of the same name in
// docs/assets/panel-redesign/panel/outlines.json.
//
// A fixture therefore has to fill the page the way its mockup is filled: the
// same boxes, the same number of records. It is also what the evidence
// screenshots are compared against, so use plausible values, not "foo".
//
// This file belongs to the implementation, not to the guards: the step that
// restyles a page adds its fixture here in the same commit.
var pageFixtures = map[string]func() any{}
