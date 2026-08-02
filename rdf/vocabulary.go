package rdf

import (
	"os"
	"path/filepath"
)

// vocabulary.ttl is the file that says what our terms mean and which of them we
// are claiming are somebody else's terms.
//
// It is a separate file from the data on purpose. Loading graph.nt gives a
// consumer the graph in our vocabulary and nothing else. Loading this as well
// adds the alignment, and the alignment is where the arguable claims are. A
// Vietnamese thông tư is an eli:LegalResource if ELI's definition of a legal
// resource covers a ministerial circular in a civil law system that is not in
// the EU, which is a reading of somebody else's specification rather than a
// fact about our data. Putting it in the data file would make it impossible to
// take our graph without also taking our reading.
//
// The alignments are stated with rdfs:subClassOf and rdfs:subPropertyOf rather
// than owl:equivalentClass, which is the weaker and the honest claim: every
// Document of ours is a legal resource, and we are saying nothing about whether
// every legal resource is one of ours.
const vocabularyTTL = `@prefix luatdo: <https://luatdo.dev/ns#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
@prefix owl: <http://www.w3.org/2002/07/owl#> .
@prefix skos: <http://www.w3.org/2004/02/skos/core#> .
@prefix dcterms: <http://purl.org/dc/terms/> .
@prefix eli: <http://data.europa.eu/eli/ontology#> .

luatdo: a owl:Ontology ;
	rdfs:label "luatdo, a Vietnamese legal knowledge graph"@en ;
	rdfs:comment "Generated from the Neo4j property graph, which is generated from the document store. This is a projection of a projection and it is not the working model."@en .

# The document layer.

luatdo:Document a owl:Class ;
	rdfs:label "Legal instrument"@en ;
	rdfs:comment "One instrument as published: a law, a decree, a circular, a decision."@en ;
	rdfs:subClassOf eli:LegalResource .

luatdo:Component a owl:Class ;
	rdfs:label "Component"@en ;
	rdfs:comment "A numbered part of an instrument: a chapter, an article, a clause, a point."@en ;
	rdfs:subClassOf eli:LegalResourceSubdivision .

luatdo:TextVersion a owl:Class ;
	rdfs:label "Text version"@en ;
	rdfs:comment "The text of a component as one instrument states it, keyed by hash. Not an ELI expression: an expression is a language version of a work, and this is a statement of the text by a particular instrument."@en .

luatdo:contains a owl:ObjectProperty ;
	rdfs:label "contains"@en ;
	rdfs:comment "An instrument or component contains a component directly beneath it."@en .

luatdo:cites a owl:ObjectProperty ;
	rdfs:label "cites"@en ;
	rdfs:comment "A citation resolved to an instrument. The method and the snippet that resolved it are on the reified statement."@en .

luatdo:amends a owl:ObjectProperty ;
	rdfs:subPropertyOf luatdo:cites ;
	rdfs:label "amends"@en .

# The subject and concept layers, which are SKOS.

luatdo:Subject a owl:Class ;
	rdfs:subClassOf skos:Concept ;
	rdfs:label "Subject"@en ;
	rdfs:comment "A filing category from the subject vocabulary, shaped like EuroVoc and not holding EuroVoc's content."@en .

luatdo:MergedConcept a owl:Class ;
	rdfs:subClassOf skos:Concept ;
	rdfs:label "Concept"@en ;
	rdfs:comment "A corpus wide concept, merged by a person from the readings of one term across instruments."@en .

luatdo:TermUse a owl:Class ;
	rdfs:label "Term use"@en ;
	rdfs:comment "One instrument's reading of one term. Deliberately not a skos:Concept: two instruments defining the same word differently are two term uses and are not one concept until somebody says so."@en .

# The norm layer, reified as instances rather than as triples.

luatdo:Norm a owl:Class ;
	rdfs:label "Norm"@en ;
	rdfs:comment "One deontic statement: a modality over an action, with a bearer, a counterparty, an object, conditions, exceptions, a deadline and a sanction. It is a node and not a triple because it is n-ary."@en .

luatdo:Condition a owl:Class ; rdfs:label "Condition"@en .
luatdo:Exception a owl:Class ; rdfs:label "Exception"@en .
luatdo:Sanction a owl:Class ; rdfs:label "Sanction"@en .

luatdo:modality a owl:DatatypeProperty ;
	rdfs:label "modality"@en ;
	rdfs:comment "obligation, prohibition, permission or power."@en .

luatdo:evidenceQuote a owl:DatatypeProperty ;
	rdfs:label "evidence quote"@en ;
	rdfs:comment "The span of the provision the statement was read from, with its character offsets beside it. A norm without one is not exported."@en .

luatdo:confidence a owl:DatatypeProperty ;
	rdfs:label "confidence"@en ;
	rdfs:comment "The extractor's own confidence. It is the model's number and it is not a measurement of the model."@en .

# The temporal layer.

luatdo:Event a owl:Class ;
	rdfs:label "Amendment event"@en ;
	rdfs:comment "One applied amendment operation, with what it closed and what it opened."@en .

luatdo:TemporalVersion a owl:Class ;
	rdfs:label "Temporal version"@en ;
	rdfs:comment "What a component said over one interval, derived from the amendment chain. Distinct from luatdo:TextVersion, which is what one instrument printed."@en .

luatdo:producesVersion a owl:ObjectProperty ;
	rdfs:label "produces version"@en ;
	rdfs:comment "An event opened this version. Named apart from luatdo:produces, which is a relation between two concepts."@en .
`

func writeVocabulary(outDir string) error {
	return os.WriteFile(filepath.Join(outDir, "vocabulary.ttl"), []byte(vocabularyTTL), 0o644)
}
