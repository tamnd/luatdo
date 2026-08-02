// Question 19. Two provisions regulate the same concept and impose incompatible
// modalities on the same actor for the same action. Find them.
//
// This is a contradiction detector and it will produce false positives, because
// two norms with the same bearer and the same action are often distinguished by a
// condition this comparison ignores. Both quotes are returned for exactly that
// reason: the pair is a candidate for a person to read, not a finding.
MATCH (a:Norm)-[:ABOUT_CONCEPT]->(c:Concept)<-[:ABOUT_CONCEPT]-(b:Norm)
WHERE a.id < b.id
  AND a.bearer = b.bearer AND a.bearer <> ''
  AND a.action = b.action AND a.action <> ''
  AND [a.norm_type, b.norm_type] IN $incompatible
MATCH (pa:Component)-[:HAS_NORM]->(a)
MATCH (pb:Component)-[:HAS_NORM]->(b)
RETURN DISTINCT c.label_vi AS concept,
       a.bearer AS actor,
       a.action AS action,
       pa.id AS provision_a, a.norm_type AS modality_a, a.evidence_quote AS quote_a,
       pb.id AS provision_b, b.norm_type AS modality_b, b.evidence_quote AS quote_b
ORDER BY concept, provision_a
LIMIT $limit
