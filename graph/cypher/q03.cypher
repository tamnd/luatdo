// Question 3. Which provincial decisions cite a central decree issued after
// them?
//
// A citation forward in time is either a drafting error or a sign the local
// instrument has been overtaken, and both are worth a look. The provincial test
// is a regular expression over the issuing body rather than a property, because
// the corpus has no field saying which level of government issued a document
// and the body's name is the only thing that does.
MATCH (citing:Document)-[:CONTAINS*0..8]->(source)-[:CITES]->(target:Document)
WHERE toLower(citing.issuing_body) =~ $provincial
  AND NOT toLower(target.issuing_body) =~ $provincial
  AND target.doc_type = $central_type
  AND citing.effective_from <> ''
  AND target.effective_from <> ''
  AND target.effective_from > citing.effective_from
RETURN DISTINCT citing.id AS provincial_decision,
       citing.issuing_body AS issued_by,
       citing.effective_from AS decided_on,
       target.id AS central_decree,
       target.effective_from AS decree_effective_from
ORDER BY decided_on, provincial_decision
LIMIT $limit
