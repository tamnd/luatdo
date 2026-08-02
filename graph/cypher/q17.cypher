// Question 17. Which norms were in force for less than a year before being
// amended?
//
// A provision that had to be changed within a year is usually one that could not
// be complied with, and the instrument that changed it is where the reason is.
// Both interval ends have to be present: a version still in force has an empty
// end date and has not yet been anything for less than a year.
MATCH (c:Component)-[:HAS_TEMPORAL_VERSION]->(v:TemporalVersion)
WHERE v.from_date <> '' AND v.to_date <> ''
  AND duration.inDays(date(v.from_date), date(v.to_date)).days < $days
MATCH (c)-[:HAS_NORM]->(n:Norm)
OPTIONAL MATCH (e:Event)-[:TERMINATES]->(v)
RETURN c.id AS component,
       n.id AS norm,
       n.action AS action,
       v.from_date AS in_force_from,
       v.to_date AS in_force_to,
       duration.inDays(date(v.from_date), date(v.to_date)).days AS days,
       e.kind AS ended_by,
       e.caused_by_doc AS ended_by_document
ORDER BY days, component
LIMIT $limit
