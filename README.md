# Repository-Tracker
Project: Third-party dependency tracker.

Summary: 
In this project, I have developed a build tool to check updates in third-party packages using Golang. 

Procedure: 
1. Retrieve info: Using bazel query, we can obtain a proto file contains all necessary build rules of dependencies. 
   We first read in the proto file, parse packages based on their rule class (including git_repository, go_package, 
   http_archive and so on), convert all of them into git repository format and then begin to check the updates. 

2. Check update: Based on the in-use version and URL information we retrieved (and parsed) from the proto file, we 
can check the updated using the git command. After this step, we can obtain all the changelogs of all dependencies. 

3. Structuralize data: Finally, we generated a markdown for the updates using a template.

