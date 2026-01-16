# GraphQL Query Optimization Example
#
# This example demonstrates Mycel's automatic GraphQL query optimization:
# 1. Field Selection - SELECT * becomes SELECT only_requested_columns
# 2. Step Skipping - Steps whose output isn't used are automatically skipped
# 3. DataLoader - N+1 queries are batched automatically
#
# NO CODE CHANGES NEEDED - Same HCL, automatic performance gains!

service {
  name    = "graphql-optimization-demo"
  version = "1.0.0"
}
