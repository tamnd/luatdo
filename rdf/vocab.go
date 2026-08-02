package rdf

import "strings"

// The vocabulary, and the part of it that is somebody else's.
//
// Two namespaces are ours. Instances live under id:, so a document identifier
// becomes an IRI a person can dereference later if this is ever published, and
// the terms live under ns:, so a property named on a node becomes a predicate.
// Everything else here is a claim that one of our terms means the same thing as
// a term someone else defined, and each of those is a claim that can be wrong.
//
// The rule used for deciding is narrow on purpose. A term is reused when the
// definition in the other vocabulary is the definition we would have written,
// and not when it is merely close. SKOS says a concept scheme is a set of
// concepts with broader and narrower links and preferred labels, which is
// exactly what the subject taxonomy and the merged concept layer are, so those
// are reused. Dublin Core title, identifier and subject are reused for the same
// reason. ELI is a different case and is handled in vocabulary.ttl rather than
// inline, because whether a Vietnamese thông tư is an eli:LegalResource is a
// question about ELI's definition, and it belongs where somebody can read the
// claim and decline to load it.
const (
	NSInstance = "https://luatdo.dev/id/"
	NSTerm     = "https://luatdo.dev/ns#"
	// NSStatement is where a reified edge lives. An edge with properties has
	// nowhere to put them in a plain triple, and RDF 1.1 reification is the
	// interchange answer that needs no extension to parse.
	NSStatement = "https://luatdo.dev/stmt/"

	nsRDF     = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	nsRDFS    = "http://www.w3.org/2000/01/rdf-schema#"
	nsXSD     = "http://www.w3.org/2001/XMLSchema#"
	nsSKOS    = "http://www.w3.org/2004/02/skos/core#"
	nsDCTerms = "http://purl.org/dc/terms/"
	nsOWL     = "http://www.w3.org/2002/07/owl#"
	nsELI     = "http://data.europa.eu/eli/ontology#"
)

// aligned maps a column name onto a predicate somebody else defined. A column
// missing from here gets a predicate in our own namespace, which is the safe
// default and the one most columns take.
var aligned = map[string]string{
	"title":           nsDCTerms + "title",
	"title_en":        nsDCTerms + "title",
	"official_number": nsDCTerms + "identifier",
	"label_vi":        nsSKOS + "prefLabel",
	"label_en":        nsSKOS + "prefLabel",
}

// alignedTypes maps a node label onto an additional rdf:type. The luatdo type
// is always emitted as well, because a consumer who wants our shape should not
// have to know that a Subject happens to be a SKOS concept.
//
// TermUse is deliberately absent. Two instruments defining one word differently
// are two term uses and are not one concept until a person says they are, and
// calling each of them a skos:Concept would put the unmerged backlog into the
// concept scheme and hide exactly the number this project reports.
var alignedTypes = map[string][]string{
	"Subject":      {nsSKOS + "Concept"},
	"Concept":      {nsSKOS + "Concept"},
	"LegalConcept": {nsSKOS + "Concept"},
}

// references names the columns that hold the identifier of another node.
//
// A property graph is content to keep a reference in a property, and half of
// these have no edge beside them: a registry class names its parent in a
// column and there is no parent relationship in the dump at all. In RDF that
// distinction does not survive contact with a consumer, because a string that
// happens to equal an IRI is not a link and nothing will follow it, so the
// projection would be a graph in name and a table in fact.
//
// The list is written out rather than inferred from the shape of the value.
// Inferring it would mean any literal that happened to match an identifier
// became a link, and a wrong edge in an interchange format is worse than a
// missing one: the missing one is visibly missing.
var references = map[string]string{
	"parent":          nsSKOS + "broader",
	"component_id":    NSTerm + "component",
	"doc_id":          NSTerm + "document",
	"scope_id":        NSTerm + "scope",
	"defined_by":      NSTerm + "definedBy",
	"caused_by_doc":   NSTerm + "causedByDocument",
	"instrument_from": NSTerm + "instrument",
	"provision_id":    NSTerm + "provision",
	"legal_basis":     NSTerm + "legalBasis",
	"procedure_id":    NSTerm + "procedure",
	"produced_by":     NSTerm + "producedBy",
	"terminated_by":   NSTerm + "terminatedBy",
	"class_id":        NSTerm + "registryClass",
	"registry_class":  NSTerm + "registryClass",
	"targets":         NSTerm + "target",
}

// alignedEdges maps a relationship type onto a predicate somebody else defined.
//
// BROADER and CONCEPT_BROADER both become skos:broader, which is the whole
// point of the rename in the projection: they are two hierarchies over two
// disjoint sets of nodes, and in SKOS that is one predicate used twice rather
// than two predicates. ABOUT_SUBJECT is dcterms:subject because that is what
// dcterms:subject is for.
var alignedEdges = map[string]string{
	"BROADER":         nsSKOS + "broader",
	"CONCEPT_BROADER": nsSKOS + "broader",
	"ABOUT_SUBJECT":   nsDCTerms + "subject",
}

// asIRI names the columns whose value is a URL rather than a string. Emitting
// one as a literal is not wrong, it is just useless: nothing will follow it.
var asIRI = map[string]string{
	"source_url": nsRDFS + "seeAlso",
}

// vietnamese names the columns that hold Vietnamese prose, so the literal
// carries a language tag.
//
// The list is written out rather than guessed. A tag is a claim about the
// content, an untagged literal claims nothing, and claiming nothing is the
// correct output for a column like method or status whose values are English
// keywords the pipeline chose. Columns ending in _vi and _en are handled by
// their suffix and are not repeated here.
var vietnamese = map[string]bool{
	"title": true, "heading": true, "text": true, "quote": true,
	"snippet": true, "evidence_quote": true, "instruction": true,
	"action": true, "bearer": true, "counterparty": true, "object": true,
	"deadline_text": true, "rationale": true, "disambiguator": true,
}

// predicate is the IRI a column becomes.
func predicate(column string) string {
	if iri, ok := asIRI[column]; ok {
		return iri
	}
	if iri, ok := aligned[column]; ok {
		return iri
	}
	if iri, ok := references[column]; ok {
		return iri
	}
	return NSTerm + camel(column)
}

// edgePredicate is the IRI a relationship type becomes.
func edgePredicate(relType string) string {
	if iri, ok := alignedEdges[relType]; ok {
		return iri
	}
	return NSTerm + camel(strings.ToLower(relType))
}

// camel turns a snake case column or relationship type into the lowerCamelCase
// a predicate is conventionally written in. HAS_LEGAL_BASIS becomes
// hasLegalBasis and label_vi becomes labelVi.
func camel(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i == 0 {
			b.WriteString(p)
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	return b.String()
}

// language is the tag a column's literals carry, empty for none.
func language(column string) string {
	switch {
	case strings.HasSuffix(column, "_vi"):
		return "vi"
	case strings.HasSuffix(column, "_en"):
		return "en"
	case vietnamese[column]:
		return "vi"
	}
	return ""
}
