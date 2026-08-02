// Question 6. Which concepts have no definition anywhere in the corpus but are
// used in more than a given number of provisions?
//
// These are the words the drafters assumed everybody knew, and they are where a
// reader who is not a Vietnamese lawyer gets stuck. Usage is counted over two
// routes because a concept is reached two ways: a norm about it, and a provision
// that defines one of its readings. The count is of distinct provisions, so a
// provision holding four norms about the same concept counts once.
MATCH (c:Concept)
WHERE NOT EXISTS {
  MATCH (t:TermUse)-[:INSTANCE_OF]->(c) WHERE t.definition_vi <> ''
}
CALL {
  WITH c
  MATCH (p:Component)-[:HAS_NORM]->(:Norm)-[:ABOUT_CONCEPT]->(c)
  RETURN p
  UNION
  WITH c
  MATCH (p:Component)-[:DEFINES_TERM]->(:TermUse)-[:INSTANCE_OF]->(c)
  RETURN p
}
WITH c, count(DISTINCT p) AS provisions
WHERE provisions > $used_in
RETURN c.id AS concept, c.label_vi AS label, provisions
ORDER BY provisions DESC, concept
LIMIT $limit
